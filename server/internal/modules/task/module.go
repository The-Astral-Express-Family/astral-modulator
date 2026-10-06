// Package task 模块：TODO 树、原子认领（claim 持有至释放/完成，无时间
// 自动过期）、regex→fuzzy 搜索、批量管理（batch.go，协议 2.1 task_batch）
// （architecture §12/§13/§17；搜索实现决策 TODO.md D7）。
package task

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

type Module struct {
	DB *gorm.DB
	// Auth 提供 workspace 级 scope 解析。
	Auth *auth.Service
	// Log 供无请求上下文路径记录错误；nil 时退回 slog.Default()。
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
	r.Delete("/tasks/{task_id}/claim", m.release)
	r.Put("/tasks/{task_id}/tags/{tag_id}", m.attachTag)
	r.Delete("/tasks/{task_id}/tags/{tag_id}", m.detachTag)
	// 依赖边（协议 2.2 task_dependencies）：from=依赖方 to=blocker（dependencies.go）。
	r.Get("/tasks/{task_id}/dependencies", m.listDependencies)
	r.Put("/tasks/{task_id}/dependencies/{dependency_task_id}", m.addDependency)
	r.Delete("/tasks/{task_id}/dependencies/{dependency_task_id}", m.removeDependency)
	// 批量管理（协议 2.1 task_batch）：整批单事务全有或全无（batch.go）。
	r.Post("/workspaces/{workspace_id}/task-trees", m.createRootTrees)
	r.Post("/tasks/{task_id}/task-trees", m.createChildTrees)
	r.Post("/workspaces/{workspace_id}/tasks/move", m.moveTasks)
	r.Post("/workspaces/{workspace_id}/tasks/batch-update", m.batchUpdate)
}

// ---- DTO ----

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
	Position        int64        `json:"position"`
	Tags            []tag.TagDTO `json:"tags"`
	ChildrenCount   int64        `json:"children_count"`
	BlockedBy       []string     `json:"blocked_by"`
	Blocks          []string     `json:"blocks"`
	Related         []string     `json:"related"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
}

// toTaskDTO 组装任务响应。v2（D15 修订 D11）：tags 恒填充（nil 兜底为空数组）、
// children_count 恒填充；position 恒填充（2.4）；2.2 起依赖视图（blocked_by/blocks/related）
// 恒填充（调用方以 depViewsForTasks 批量装填，无边时空切片在此兜底）。
func toTaskDTO(t model.Task, tags []tag.TagDTO, childCount int64, deps depViews) taskDTO {
	if tags == nil {
		tags = []tag.TagDTO{}
	}
	if deps.blockedBy == nil {
		deps.blockedBy = []string{}
	}
	if deps.blocks == nil {
		deps.blocks = []string{}
	}
	if deps.related == nil {
		deps.related = []string{}
	}
	return taskDTO{
		ID: t.ID, WorkspaceID: t.WorkspaceID, ParentID: t.ParentID,
		Title: t.Title, Description: t.Description,
		Status: t.Status, Priority: t.Priority,
		AssigneeActorID: t.AssigneeActorID, Revision: t.Revision,
		Position: t.Position,
		Tags:     tags, ChildrenCount: childCount,
		BlockedBy: deps.blockedBy, Blocks: deps.blocks, Related: deps.related,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// LoadForWorkspace 是「task 主语」端点的统一入口：加载任务（查无此行用专用码
// TASK_NOT_FOUND，DB 故障如实 500）并执行 workspace 级 scope 校验。以 ctx 为介质，
// 供本模块 handler/service 与跨模块消费方（message 线程端点）共用——
// task 的加载与 404 语义全仓只有这一处实现。
func (m *Module) LoadForWorkspace(ctx context.Context, p *auth.Principal, taskID, need string) (*model.Task, *httpx.APIError) {
	var t model.Task
	if apiErr := store.First(m.DB.WithContext(ctx), &t,
		&httpx.APIError{Status: 404, Code: httpx.CodeTaskNotFound, Message: "task not found"},
		"id = ?", taskID); apiErr != nil {
		return nil, apiErr
	}
	if _, apiErr := m.Auth.RequireWorkspaceScopes(ctx, p, t.WorkspaceID, need); apiErr != nil {
		return nil, apiErr
	}
	return &t, nil
}

// requireTask 是 LoadForWorkspace 的 handler 侧便捷入口。
func (m *Module) requireTask(r *http.Request, taskID string, need string) (*model.Task, *httpx.APIError) {
	return m.LoadForWorkspace(r.Context(), auth.PrincipalFrom(r.Context()), taskID, need)
}

// ---- CRUD：创建走容器端点（children.go）；读取集合走容器集合（children.go）----

// search：workspace 级平面查询（v2 task-search）。结构化（tag/status/assignee/
// blocked/blocked_by）与内容（regex/fuzzy）平权；至少一个条件，防全量 dump
// （候选集另有封顶，D7）。blocked 仅在取 true 时计入条件并过滤（false 不表达
// 过滤意图，计入等于无过滤全量枚举）；与其他条件并用时非法取值被忽略。
func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	if q.Get("regex") == "" && q.Get("fuzzy") == "" && q.Get("tag") == "" &&
		q.Get("status") == "" && q.Get("assignee") == "" &&
		q.Get("blocked") != "true" && q.Get("blocked_by") == "" {
		httpx.WriteError(w, r, httpx.Invalid("at least one filter (regex/fuzzy/tag/status/assignee/blocked/blocked_by) is required"))
		return
	}
	results, next, apiErr := m.Search(r.Context(), wsID, SearchParams{
		Regex:     q.Get("regex"),
		Fuzzy:     q.Get("fuzzy"),
		ParentID:  q.Get("parent_id"),
		Tag:       q.Get("tag"),
		Status:    q.Get("status"),
		Assignee:  q.Get("assignee"),
		Blocked:   q.Get("blocked") == "true",
		BlockedBy: q.Get("blocked_by"),
		Limit:     httpx.ParseLimit(q.Get("limit"), 50, 200),
		Cursor:    q.Get("cursor"),
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
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(*t, m.loadTags(r, t.ID),
		m.childCount(r.Context(), t.ID), m.depView(r.Context(), t.ID)))
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
		ExpectedRevision *int64          `json:"expected_revision"`
		Title            *string         `json:"title"`
		Description      *string         `json:"description"`
		Status           *string         `json:"status"`
		Priority         *string         `json:"priority"`
		ParentID         **string        `json:"parent_id"`         // JSON null 与缺席同义 = 不改（encoding/json 对 **string 的 null 置外层 nil）；移动走 move 端点
		AssigneeActorID  json.RawMessage `json:"assignee_actor_id"` // 出现于请求体才生效：显式 null = 清空，缺席 = 不改
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.ExpectedRevision == nil || *in.ExpectedRevision != t.Revision {
		httpx.WriteError(w, r, revisionConflict(t.Revision))
		return
	}

	updates := map[string]any{
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
			if err := m.validateParent(r.Context(), t.WorkspaceID, *parent); err != nil {
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
		assignee, err := assigneeFromJSON(in.AssigneeActorID)
		if err != nil {
			httpx.WriteError(w, r, httpx.Invalid("assignee_actor_id must be an actor id or null"))
			return
		}
		updates["assignee_actor_id"] = *assignee // 内层 nil = 清空（GORM map nil → NULL，同 release）
	}

	// 单事务：条件更新（乐观并发）+ audit + outbox。业务写与事件
	// 分离提交会让客户端在“已生效但无事件”的窗口里重试撞 REVISION_CONFLICT。
	var fresh model.Task
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := bumpRevisionTx(tx, t.ID, t.Revision, updates); err != nil {
			return err
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
		return outbox.EmitTx(tx, outbox.TypeTaskUpdated, t.WorkspaceID, p.ActorID, fresh.Revision,
			map[string]any{"task_id": t.ID})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(fresh, m.loadTags(r, t.ID),
		m.childCount(r.Context(), t.ID), m.depView(r.Context(), t.ID)))
}

// assigneeFromJSON 把请求体里的 assignee_actor_id 归一为 service 层三态
// (**string)：显式 null → 内层 nil（清空）；其余 → 内层指到该值。缺席由
// 调用方先判 json.RawMessage 是否出现。encoding/json 对 **string 的 null
// 会置外层指针为 nil、与缺席不可区分——三态只能在 RawMessage 层分辨
// （PATCH 与 batch-update 的 HTTP 层共用此归一）。
func assigneeFromJSON(raw json.RawMessage) (**string, error) {
	if string(raw) == "null" {
		inner := (*string)(nil)
		return &inner, nil
	}
	var v *string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
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
// 授权（task:claim，经 LoadForWorkspace）内聚在服务层——与 AttachTag/DetachTag
// 同规矩，任何入口复用本核心都不会绕过校验。单事务内完成：revision 校验 →
// assignee 条件更新（互斥）→ audit + outbox。
//
// 互斥语义（TODO.md §9 2026-09-30，租约时间维度拆除）：认领持有至 release
// 或任务完成，无时间自动过期；条件更新 WHERE assignee 为空或自己——他人
// 持有时 0 行命中 → TASK_ALREADY_CLAIMED，仅 revision 漂移 → REVISION_CONFLICT。
func (m *Module) Claim(ctx context.Context, p *auth.Principal, taskID string, expectedRevision *int64) (*model.Task, error) {
	current, apiErr := m.LoadForWorkspace(ctx, p, taskID, auth.ScopeTaskClaim)
	if apiErr != nil {
		return nil, apiErr
	}

	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 条件更新防并发：revision 以 expected_revision 为准（缺省=装载时快照），
		// 且无人认领（或自己重复认领）才生效。
		// sqlite 串行写天然原子；PG 靠行锁 + WHERE 重评兜底。
		expected := current.Revision
		if expectedRevision != nil {
			expected = *expectedRevision
		}
		res := tx.Model(&model.Task{}).
			Where("id = ? AND revision = ? AND (assignee_actor_id IS NULL OR assignee_actor_id = ?)",
				taskID, expected, p.ActorID).
			Updates(map[string]any{
				"assignee_actor_id": p.ActorID,
				"status":            "in_progress",
				"updated_by":        p.ActorID,
				"revision":          expected + 1,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 区分竞争来源：他人持有 → TASK_ALREADY_CLAIMED；仅 revision 漂移 →
			// REVISION_CONFLICT（details 带当前 revision，供 re-read-retry）。
			var now model.Task
			if err := tx.First(&now, "id = ?", taskID).Error; err != nil {
				return err
			}
			if now.AssigneeActorID != nil && *now.AssigneeActorID != p.ActorID {
				return httpx.ConflictWith(httpx.CodeTaskAlreadyClaimed,
					"task is already claimed by another actor", map[string]any{"task_id": taskID})
			}
			return revisionConflict(now.Revision)
		}
		// audit + outbox 同事务。
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: current.WorkspaceID, ActorID: p.ActorID,
			Action: "task.claim", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeTaskClaimed, current.WorkspaceID, p.ActorID, expected+1,
			map[string]any{"task_id": taskID})
	})
	if err != nil {
		return nil, err
	}
	// 认领已提交，读回失败不应整体报错：用事务内已知状态兜底构造。
	var fresh model.Task
	if e := m.DB.WithContext(ctx).First(&fresh, "id = ?", taskID).Error; e != nil {
		fresh = *current
		fresh.AssigneeActorID = &p.ActorID
		fresh.Status = "in_progress"
		fresh.Revision = current.Revision + 1
	}
	return &fresh, nil
}

func (m *Module) claim(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	// 授权在 Claim 内部（LoadForWorkspace）；此处只负责解码与组装。
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	fresh, err := m.Claim(r.Context(), p, taskID, in.ExpectedRevision)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	dto := toTaskDTO(*fresh, m.loadTags(r, taskID),
		m.childCount(r.Context(), taskID), m.depView(r.Context(), taskID))
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"task": dto})
}

// Release 释放认领核心（HTTP handler 与测试共用，与 Claim 同构）：claimant
// 本人可释放；他人需要 task:override（human 强制干预，architecture §22 候选
// 动作）。未认领 → 幂等成功（不 bump revision 不发事件）。
func (m *Module) Release(ctx context.Context, p *auth.Principal, taskID string) error {
	t, apiErr := m.LoadForWorkspace(ctx, p, taskID, auth.ScopeTaskClaim)
	if apiErr != nil {
		return apiErr
	}
	if t.AssigneeActorID == nil {
		return nil
	}
	if *t.AssigneeActorID != p.ActorID {
		scopes, _ := m.Auth.WorkspaceScopes(ctx, p, t.WorkspaceID)
		if !scopes[auth.ScopeTaskOverride] {
			return httpx.Forbidden("only the claimant or task:override can release")
		}
	}
	// 与 claim/update 同构：条件更新 + 0 行 = 并发修改 → REVISION_CONFLICT。
	return m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpRevisionTx(tx, taskID, t.Revision, releaseOwnershipFields(t.Status)); err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.release", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeTaskReleased, t.WorkspaceID, p.ActorID, t.Revision+1,
			map[string]any{"task_id": taskID})
	})
}

func (m *Module) release(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	p := auth.PrincipalFrom(r.Context())
	if err := m.Release(r.Context(), p, taskID); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

// releaseOwnershipFields 释放任务归属的字段集（release 使用）：
// 清 assignee；in_progress 回退 open。revision 由调用方决定是否写入。
func releaseOwnershipFields(status string) map[string]any {
	updates := map[string]any{"assignee_actor_id": nil}
	if status == "in_progress" {
		updates["status"] = "open"
	}
	return updates
}

// bumpRevisionTx 条件 revision bump（乐观并发单点实现）：WHERE id AND
// revision=expected，0 行 = 并发修改 → 事务内重读当前行并返回
// REVISION_CONFLICT（details.current_revision 供客户端 re-read-retry）。
// updates 不含 revision，由本函数统一写 expected+1。
func bumpRevisionTx(tx *gorm.DB, taskID string, expected int64, updates map[string]any) error {
	if updates == nil {
		updates = map[string]any{}
	}
	updates["revision"] = expected + 1
	res := tx.Model(&model.Task{}).Where("id = ? AND revision = ?", taskID, expected).Updates(updates)
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
	return nil
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
