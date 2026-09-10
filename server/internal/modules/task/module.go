// Package task 模块：TODO 树、原子 claim/lease、regex→fuzzy 搜索
// （architecture §12/§13/§17；搜索实现决策 TODO.md D7）。
package task

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/background"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// Lease 时长钳制边界（openapi TaskClaimInput.lease_seconds 的服务端约束）。
// 归属本模块：lease 是 task 领域概念，与 auth 无关。
var (
	LeaseDefault = 5 * time.Minute
	LeaseMin     = 30 * time.Second
	LeaseMax     = time.Hour
)

type Module struct {
	DB *gorm.DB
	// Auth 提供 workspace 级 scope 解析。
	Auth *auth.Service
	// Log 供后台清扫等无请求上下文路径记录错误；nil 时退回 slog.Default()。
	Log *slog.Logger
}

func (m *Module) logger() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

func (m *Module) RegisterRoutes(r chi.Router) {
	// 容器化任务树（D15，v2）：凡容器，GET/POST <container>/children。
	r.Get("/workspaces/{workspace_id}/children", m.listWorkspaceChildren)
	r.Post("/workspaces/{workspace_id}/children", m.createRoot)
	r.Get("/tasks/{task_id}/children", m.listTaskChildren)
	r.Post("/tasks/{task_id}/children", m.createChild)
	r.Get("/workspaces/{workspace_id}/task-search", m.search)
	r.Get("/tasks/{task_id}", m.get)
	r.Patch("/tasks/{task_id}", m.update)
	r.Post("/tasks/{task_id}/claim", m.claim)
	r.Post("/tasks/{task_id}/lease/renew", m.renewLease)
	r.Delete("/tasks/{task_id}/lease", m.release)
	r.Put("/tasks/{task_id}/tags/{tag_id}", m.attachTag)
	r.Delete("/tasks/{task_id}/tags/{tag_id}", m.detachTag)
	r.Get("/tasks/{task_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		httpx.NotImplemented(w, r, "task.messages.list", "phase-4", "api/openapi.yaml /tasks/{task_id}/messages")
	})
}

// StartSweeper 启动租约清扫 goroutine（由 app 装配调用，ctx 取消即退出）。
func (m *Module) StartSweeper(ctx context.Context, every time.Duration) {
	background.RunEvery(ctx, every, func(ctx context.Context) {
		m.sweepOnce(ctx)
	})
}

// sweepOnce 清扫过期租约。删除必须是条件删除（expires_at < now）：
// 快照查询与删除之间 holder 可能已续租，无条件删除会误杀有效租约。
func (m *Module) sweepOnce(ctx context.Context) {
	var expired []model.TaskLease
	err := m.DB.WithContext(ctx).Where("expires_at < ?", time.Now()).Limit(100).Find(&expired).Error
	if err != nil {
		m.logger().Error("lease sweep query failed", "err", err)
		return
	}
	for _, lease := range expired {
		err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Where("task_id = ? AND expires_at < ?", lease.TaskID, time.Now()).Delete(&model.TaskLease{})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				// 快照后被续租/释放，本轮跳过。
				return nil
			}
			var t model.Task
			if err := tx.First(&t, "id = ?", lease.TaskID).Error; err != nil {
				return err
			}
			updates := map[string]any{"assignee_actor_id": nil, "revision": t.Revision + 1}
			if t.Status == "in_progress" {
				updates["status"] = "open"
			}
			if err := tx.Model(&model.Task{}).Where("id = ?", t.ID).Updates(updates).Error; err != nil {
				return err
			}
			return event.EmitTx(tx, event.TypeTaskLeaseExpired, t.WorkspaceID, lease.HolderActorID, t.Revision+1,
				map[string]any{"task_id": t.ID, "previous_holder": lease.HolderActorID})
		})
		if err != nil {
			m.logger().Error("lease sweep failed", "task_id", lease.TaskID, "err", err)
		}
	}
}

// ---- DTO ----

type leaseDTO struct {
	HolderActorID string `json:"holder_actor_id"`
	ExpiresAt     string `json:"expires_at"`
	RenewedAt     string `json:"renewed_at,omitempty"`
}

type taskDTO struct {
	ID              string       `json:"id"`
	WorkspaceID     string       `json:"workspace_id"`
	ParentID        *string      `json:"parent_id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Status          string       `json:"status"`
	Priority        string       `json:"priority"`
	AssigneeActorID *string      `json:"assignee_actor_id"`
	Revision        int64        `json:"revision"`
	Tags            []tag.TagDTO `json:"tags"`
	ChildrenCount   int64        `json:"children_count"`
	Lease           *leaseDTO    `json:"lease"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
}

// toTaskDTO 组装任务响应。v2（D15 修订 D11）：tags 恒填充（nil 兜底为空数组）、
// children_count 恒填充；lease 仅 get/claim 填充非空。
func toTaskDTO(t model.Task, lease *model.TaskLease, tags []tag.TagDTO, childCount int64) taskDTO {
	if tags == nil {
		tags = []tag.TagDTO{}
	}
	dto := taskDTO{
		ID: t.ID, WorkspaceID: t.WorkspaceID, ParentID: t.ParentID,
		Title: t.Title, Description: t.Description,
		Status: t.Status, Priority: t.Priority,
		AssigneeActorID: t.AssigneeActorID, Revision: t.Revision,
		Tags: tags, ChildrenCount: childCount,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if lease != nil && lease.ExpiresAt.After(time.Now()) {
		dto.Lease = &leaseDTO{
			HolderActorID: lease.HolderActorID,
			ExpiresAt:     lease.ExpiresAt.UTC().Format(time.RFC3339),
			RenewedAt:     lease.RenewedAt.UTC().Format(time.RFC3339),
		}
	}
	return dto
}

// requireTask：加载任务 + workspace scope 校验。
func (m *Module) requireTask(r *http.Request, taskID string, need string) (*model.Task, *httpx.APIError) {
	var t model.Task
	err := m.DB.WithContext(r.Context()).First(&t, "id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 404, Code: httpx.CodeTaskNotFound, Message: "task not found"}
	}
	if err != nil {
		return nil, httpx.Internal("task lookup failed")
	}
	p := auth.PrincipalFrom(r.Context())
	if _, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, t.WorkspaceID, need); apiErr != nil {
		return nil, apiErr
	}
	return &t, nil
}

// ---- CRUD：创建走容器端点（children.go）；读取集合走容器集合（children.go）----

// search：workspace 级平面查询（v2 task-search）。结构化（tag/status/assignee）
// 与内容（regex/fuzzy）平权；至少一个条件，防全量 dump（候选集另有封顶，D7）。
func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	if q.Get("regex") == "" && q.Get("fuzzy") == "" && q.Get("tag") == "" &&
		q.Get("status") == "" && q.Get("assignee") == "" {
		httpx.WriteError(w, r, httpx.Invalid("at least one filter (regex/fuzzy/tag/status/assignee) is required"))
		return
	}
	results, next, apiErr := m.Search(r.Context(), wsID, SearchParams{
		Regex:    q.Get("regex"),
		Fuzzy:    q.Get("fuzzy"),
		ParentID: q.Get("parent_id"),
		Tag:      q.Get("tag"),
		Status:   q.Get("status"),
		Assignee: q.Get("assignee"),
		Limit:    parseLimit(q.Get("limit"), 50, 200),
		Cursor:   q.Get("cursor"),
	})
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(results, next))
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	t, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var lease model.TaskLease
	hasLease := m.DB.WithContext(r.Context()).First(&lease, "task_id = ?", t.ID).Error == nil
	if hasLease && lease.ExpiresAt.Before(time.Now()) {
		hasLease = false
	}
	var leasePtr *model.TaskLease
	if hasLease {
		leasePtr = &lease
	}
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(*t, leasePtr, m.loadTags(r, t.ID), m.childCount(r.Context(), t.ID)))
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	t, apiErr := m.requireTask(r, taskID, auth.ScopeTaskWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		ExpectedRevision *int64   `json:"expected_revision"`
		Title            *string  `json:"title"`
		Description      *string  `json:"description"`
		Status           *string  `json:"status"`
		Priority         *string  `json:"priority"`
		ParentID         **string `json:"parent_id"` // 三态：null=不改，非null带内层
		AssigneeActorID  **string `json:"assignee_actor_id"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.ExpectedRevision == nil || *in.ExpectedRevision != t.Revision {
		httpx.WriteError(w, r, revisionConflict(t.Revision))
		return
	}

	updates := map[string]any{
		"revision":   t.Revision + 1,
		"updated_by": p.ActorID,
	}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" || len(title) > 500 {
			httpx.WriteError(w, r, httpx.Invalid("title required (1-500 chars)"))
			return
		}
		updates["title"] = title
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.Status != nil {
		if !validStatus(*in.Status) {
			httpx.WriteError(w, r, httpx.Invalid("invalid status"))
			return
		}
		updates["status"] = *in.Status
	}
	if in.Priority != nil {
		if !validPriority(*in.Priority) {
			httpx.WriteError(w, r, httpx.Invalid("invalid priority"))
			return
		}
		updates["priority"] = *in.Priority
	}
	if in.ParentID != nil {
		parent := *in.ParentID // *string
		if parent != nil {
			var parentTask model.Task
			err := m.DB.WithContext(r.Context()).First(&parentTask, "id = ?", *parent).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && parentTask.WorkspaceID != t.WorkspaceID) {
				httpx.WriteError(w, r, httpx.Invalid("parent must exist in the same workspace"))
				return
			}
			if err != nil {
				httpx.RespondError(w, r, err)
				return
			}
			// 循环检测：从新 parent 向上走祖先链，遇到自己即循环。
			// （recursive CTE 在 sqlite/PG 双方言兼容，此处用应用层遍历，树深有限。）
			cyclic, err := m.checkCycle(r, t.ID, *parent)
			if err != nil {
				httpx.RespondError(w, r, err)
				return
			}
			if cyclic {
				httpx.WriteError(w, r, httpx.Invalid("parent change would create a cycle"))
				return
			}
		}
		updates["parent_id"] = parent
	}
	if in.AssigneeActorID != nil {
		updates["assignee_actor_id"] = *in.AssigneeActorID
	}

	// 单事务：条件更新（乐观并发）+ audit + outbox。业务写与事件
	// 分离提交会让客户端在“已生效但无事件”的窗口里重试撞 REVISION_CONFLICT。
	var fresh model.Task
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Task{}).
			Where("id = ? AND revision = ?", t.ID, t.Revision).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var current model.Task
			if e := tx.First(&current, "id = ?", t.ID).Error; e != nil {
				return e
			}
			return revisionConflict(current.Revision)
		}
		if err := tx.First(&fresh, "id = ?", t.ID).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.update", Outcome: "allowed",
			TargetType: "task", TargetID: t.ID,
			Details: map[string]any{"new_revision": fresh.Revision},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskUpdated, t.WorkspaceID, p.ActorID, fresh.Revision,
			map[string]any{"task_id": t.ID})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(fresh, nil, m.loadTags(r, t.ID), m.childCount(r.Context(), t.ID)))
}

// checkCycle 沿 newParent 向上遍历祖先链，返回是否形成环。
// 祖先行缺失（脏数据/并发删除）视为无环；真实 DB 错误向上返回。
func (m *Module) checkCycle(r *http.Request, taskID, newParent string) (bool, error) {
	current := newParent
	for depth := 0; depth < 64 && current != ""; depth++ {
		if current == taskID {
			return true, nil
		}
		var t model.Task
		err := m.DB.WithContext(r.Context()).Select("parent_id").First(&t, "id = ?", current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if t.ParentID == nil {
			return false, nil
		}
		current = *t.ParentID
	}
	return false, nil
}

// Claim 原子认领核心（HTTP handler 与测试共用；roadmap spike 验收项）。
// 单事务内完成：revision 校验 → 抢租约 → 条件更新任务 → audit + outbox。
func (m *Module) Claim(ctx context.Context, p *auth.Principal, taskID string, expectedRevision *int64, leaseSeconds int) (*model.Task, *model.TaskLease, error) {
	var current model.Task
	err := m.DB.WithContext(ctx).First(&current, "id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, &httpx.APIError{Status: 404, Code: httpx.CodeTaskNotFound, Message: "task not found"}
	}
	if err != nil {
		return nil, nil, err
	}
	lease := time.Duration(leaseSecondsValue(leaseSeconds)) * time.Second

	var claimedLease model.TaskLease
	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. revision 校验（可选但推荐）。
		if expectedRevision != nil && *expectedRevision != current.Revision {
			return revisionConflict(current.Revision)
		}
		// 2. 原子抢租约：条件删除旧（过期或自己持有）→ 插入。
		//    并发下第二个事务的 INSERT 会因主键冲突失败 → TASK_ALREADY_CLAIMED。
		//    sqlite 串行写天然原子；PG 靠主键唯一约束兜底。
		res := tx.Where("task_id = ? AND (expires_at < ? OR holder_actor_id = ?)",
			taskID, time.Now(), p.ActorID).Delete(&model.TaskLease{})
		if res.Error != nil {
			return res.Error
		}
		now := time.Now()
		claimedLease = model.TaskLease{
			TaskID:        taskID,
			HolderActorID: p.ActorID,
			ExpiresAt:     now.Add(lease),
			RenewedAt:     now,
		}
		if err := tx.Create(&claimedLease).Error; err != nil {
			// 主键冲突 = 别人持有有效租约。
			if store.IsUniqueViolation(err) {
				return httpx.ConflictWith(httpx.CodeTaskAlreadyClaimed,
					"task is already claimed by another actor", map[string]any{"task_id": taskID})
			}
			return err
		}
		// 3. 更新任务归属（同样条件更新防并发）。
		updates := map[string]any{
			"assignee_actor_id": p.ActorID,
			"status":            "in_progress",
			"revision":          current.Revision + 1,
			"updated_by":        p.ActorID,
		}
		res = tx.Model(&model.Task{}).Where("id = ? AND revision = ?", taskID, current.Revision).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return revisionConflict(current.Revision)
		}
		// 4. audit + outbox 同事务。
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: current.WorkspaceID, ActorID: p.ActorID,
			Action: "task.claim", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
			Details: map[string]any{"lease_expires_at": claimedLease.ExpiresAt.UTC().Format(time.RFC3339)},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskClaimed, current.WorkspaceID, p.ActorID, current.Revision+1,
			map[string]any{"task_id": taskID, "lease_expires_at": claimedLease.ExpiresAt.UTC().Format(time.RFC3339)})
	})
	if err != nil {
		return nil, nil, err
	}
	// 认领已提交，读回失败不应整体报错：用事务内已知状态兜底构造。
	var fresh model.Task
	if e := m.DB.WithContext(ctx).First(&fresh, "id = ?", taskID).Error; e != nil {
		fresh = current
		fresh.AssigneeActorID = &p.ActorID
		fresh.Status = "in_progress"
		fresh.Revision = current.Revision + 1
	}
	return &fresh, &claimedLease, nil
}

func (m *Module) claim(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	if _, apiErr := m.requireTask(r, taskID, auth.ScopeTaskClaim); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		ExpectedRevision *int64 `json:"expected_revision"`
		LeaseSeconds     int    `json:"lease_seconds"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	fresh, claimedLease, err := m.Claim(r.Context(), p, taskID, in.ExpectedRevision, in.LeaseSeconds)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	dto := toTaskDTO(*fresh, claimedLease, m.loadTags(r, taskID), m.childCount(r.Context(), taskID))
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"task": dto, "lease": dto.Lease})
}

func (m *Module) renewLease(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	if _, apiErr := m.requireTask(r, taskID, auth.ScopeTaskClaim); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		LeaseSeconds int `json:"lease_seconds"`
	}
	// body 可选（全默认时长）；非 JSON body 视为未携带。
	_ = json.NewDecoder(r.Body).Decode(&in)
	lease := time.Duration(leaseSecondsValue(in.LeaseSeconds)) * time.Second

	now := time.Now()
	var existing model.TaskLease
	err := m.DB.WithContext(r.Context()).First(&existing, "task_id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && existing.ExpiresAt.Before(now)) {
		httpx.WriteError(w, r, httpx.Conflict(httpx.CodeTaskLeaseExpired, "lease expired; claim again"))
		return
	}
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	if existing.HolderActorID != p.ActorID {
		httpx.WriteError(w, r, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "only the lease holder can renew"})
		return
	}
	updates := map[string]any{"expires_at": now.Add(lease), "renewed_at": now}
	if err := m.DB.WithContext(r.Context()).Model(&model.TaskLease{}).Where("task_id = ?", taskID).Updates(updates).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	existing.ExpiresAt = now.Add(lease)
	existing.RenewedAt = now
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{
		"holder_actor_id": existing.HolderActorID,
		"expires_at":      existing.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (m *Module) release(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	t, apiErr := m.requireTask(r, taskID, auth.ScopeTaskClaim)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var lease model.TaskLease
	err := m.DB.WithContext(r.Context()).First(&lease, "task_id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// holder 可释放；他人需要 task:override（human 强制干预，architecture §22 候选动作）。
	if lease.HolderActorID != p.ActorID {
		scopes, _ := m.Auth.WorkspaceScopes(r.Context(), p, t.WorkspaceID)
		if !scopes[auth.ScopeTaskOverride] {
			httpx.WriteError(w, r, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "only holder or task:override can release"})
			return
		}
	}
	// 与 claim/update 同构：条件更新 + 0 行 = 并发修改 → REVISION_CONFLICT，
	// 租约删除随事务回滚，不出现“租约没了但任务没放开”。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&model.TaskLease{}).Error; err != nil {
			return err
		}
		updates := map[string]any{"assignee_actor_id": nil, "revision": t.Revision + 1}
		if t.Status == "in_progress" {
			updates["status"] = "open"
		}
		res := tx.Model(&model.Task{}).Where("id = ? AND revision = ?", taskID, t.Revision).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var current model.Task
			if e := tx.First(&current, "id = ?", taskID).Error; e != nil {
				return e
			}
			return revisionConflict(current.Revision)
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.release", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskReleased, t.WorkspaceID, p.ActorID, t.Revision+1,
			map[string]any{"task_id": taskID})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func leaseSecondsValue(in int) int {
	switch {
	case in <= 0:
		return int(LeaseDefault.Seconds())
	case in < int(LeaseMin.Seconds()):
		return int(LeaseMin.Seconds())
	case in > int(LeaseMax.Seconds()):
		return int(LeaseMax.Seconds())
	default:
		return in
	}
}

// revisionConflict 统一构造 409 REVISION_CONFLICT（details 携带当前 revision，
// CLI/GUI 据此做 re-read-retry）。
func revisionConflict(current int64) *httpx.APIError {
	return httpx.ConflictWith(httpx.CodeRevisionConflict, "revision mismatch",
		map[string]any{"current_revision": current})
}

func validStatus(s string) bool {
	switch s {
	case "open", "in_progress", "blocked", "review", "done", "cancelled":
		return true
	}
	return false
}

func validPriority(p string) bool {
	switch p {
	case "low", "normal", "high", "urgent":
		return true
	}
	return false
}

func parseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
