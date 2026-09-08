// Package task 模块：TODO 树、原子 claim/lease（architecture §12/§17）。
// 搜索（regex→fuzzy）与 tags proposal 属 phase-3 后续（见 TODO.md T-task-6/7）。
package task

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
)

type Module struct {
	DB    *gorm.DB
	Audit *audit.GormRecorder
	Hub   *event.Hub
	Auth  *auth.Service
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspace_id}/tasks", m.create)
	r.Get("/workspaces/{workspace_id}/tasks", m.list)
	// search 已实装（regex→fuzzy 管线见 search.go，决策 TODO.md D7）。
	r.Get("/workspaces/{workspace_id}/tasks/search", m.search)
	r.Get("/tasks/{task_id}", m.get)
	r.Patch("/tasks/{task_id}", m.update)
	r.Post("/tasks/{task_id}/claim", m.claim)
	r.Post("/tasks/{task_id}/lease/renew", m.renewLease)
	r.Delete("/tasks/{task_id}/lease", m.release)
	r.Get("/tasks/{task_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		httpx.NotImplemented(w, r, "task.messages.list", "phase-4", "api/openapi.yaml /tasks/{task_id}/messages")
	})
}

// StartSweeper 启动租约清扫 goroutine（由 app 装配调用）。
func (m *Module) StartSweeper(stop <-chan struct{}, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.sweepOnce()
			}
		}
	}()
}

func (m *Module) sweepOnce() {
	var expired []model.TaskLease
	err := m.DB.Where("expires_at < ?", time.Now()).Limit(100).Find(&expired).Error
	if err != nil {
		return
	}
	for _, lease := range expired {
		err := m.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("task_id = ?", lease.TaskID).Delete(&model.TaskLease{}).Error; err != nil {
				return err
			}
			var t model.Task
			if err := tx.First(&t, "id = ?", lease.TaskID).Error; err != nil {
				return err
			}
			updates := map[string]any{"assignee_actor_id": nil, "revision": t.Revision + 1, "updated_at": time.Now()}
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
			continue
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
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	ParentID        *string   `json:"parent_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	AssigneeActorID *string   `json:"assignee_actor_id"`
	Revision        int64     `json:"revision"`
	Lease           *leaseDTO `json:"lease"`
	CreatedAt       string    `json:"created_at"`
	UpdatedAt       string    `json:"updated_at"`
}

func toTaskDTO(t model.Task, lease *model.TaskLease) taskDTO {
	dto := taskDTO{
		ID: t.ID, WorkspaceID: t.WorkspaceID, ParentID: t.ParentID,
		Title: t.Title, Description: t.Description,
		Status: t.Status, Priority: t.Priority,
		AssigneeActorID: t.AssigneeActorID, Revision: t.Revision,
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
		return nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "task lookup failed"}
	}
	p := auth.PrincipalFrom(r.Context())
	if _, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, t.WorkspaceID, need); apiErr != nil {
		return nil, apiErr
	}
	return &t, nil
}

// ---- CRUD ----

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeTaskWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		ParentID    *string `json:"parent_id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Priority    string  `json:"priority"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 500 {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "title required (1-500 chars)"})
		return
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !validPriority(in.Priority) {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid priority"})
		return
	}
	if in.ParentID != nil {
		var parent model.Task
		err := m.DB.WithContext(r.Context()).First(&parent, "id = ?", *in.ParentID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && parent.WorkspaceID != wsID) {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "parent must exist in the same workspace"})
			return
		}
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
	}

	t := model.Task{
		ID: ids.New(ids.Task), WorkspaceID: wsID, ParentID: in.ParentID,
		Title: in.Title, Description: in.Description,
		Status: "open", Priority: in.Priority, Revision: 1,
		CreatedBy: p.ActorID,
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "task.create", Outcome: "allowed",
			TargetType: "task", TargetID: t.ID,
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskCreated, wsID, p.ActorID, 1,
			map[string]any{"task_id": t.ID, "title": t.Title})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, toTaskDTO(t, nil))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeTaskRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	query := m.DB.WithContext(r.Context()).Model(&model.Task{}).Where("workspace_id = ?", wsID)
	if v := q.Get("parent_id"); v != "" {
		query = query.Where("parent_id = ?", v)
	}
	if v := q.Get("status"); v != "" {
		query = query.Where("status = ?", v)
	}
	if v := q.Get("assignee"); v != "" {
		query = query.Where("assignee_actor_id = ?", v)
	}
	// cursor 分页：base64({c: created_at, i: id})，稳定序 (created_at, id)。
	// 行值比较用 OR 展开保持 sqlite/PG 双方言兼容。
	if v := q.Get("cursor"); v != "" {
		ca, id, err := decodeCursor(v)
		if err != nil {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid cursor"})
			return
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", ca, ca, id)
	}
	limit := parseLimit(q.Get("limit"), 50, 200)

	var rows []model.Task
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	items := make([]taskDTO, 0, len(rows))
	for _, t := range rows {
		items = append(items, toTaskDTO(t, nil))
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursorPtr(next)})
}

// search：regex 过滤 → fuzzy 排序（architecture §13 语义；CLI --regex/--fuzzy 双参数）。
func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeTaskRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	if q.Get("regex") == "" && q.Get("fuzzy") == "" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed,
			Message: "at least one of regex/fuzzy is required"})
		return
	}
	results, next, apiErr := m.Search(r.Context(), wsID, SearchParams{
		Regex:    q.Get("regex"),
		Fuzzy:    q.Get("fuzzy"),
		ParentID: q.Get("parent_id"),
		Tag:      q.Get("tag"),
		Status:   q.Get("status"),
		Limit:    parseLimit(q.Get("limit"), 50, 200),
		Cursor:   q.Get("cursor"),
	})
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": results, "next_cursor": nextCursorPtr(next)})
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
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(*t, leasePtr))
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
		httpx.WriteError(w, r, &httpx.APIError{
			Status: http.StatusConflict, Code: httpx.CodeRevisionConflict,
			Message: "revision mismatch",
			Details: map[string]any{"current_revision": t.Revision},
		})
		return
	}

	updates := map[string]any{
		"revision":   t.Revision + 1,
		"updated_at": time.Now(),
		"updated_by": p.ActorID,
	}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" || len(title) > 500 {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "title required (1-500 chars)"})
			return
		}
		updates["title"] = title
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if in.Status != nil {
		if !validStatus(*in.Status) {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid status"})
			return
		}
		updates["status"] = *in.Status
	}
	if in.Priority != nil {
		if !validPriority(*in.Priority) {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid priority"})
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
				httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "parent must exist in the same workspace"})
				return
			}
			if err != nil {
				httpx.RespondError(w, r, err)
				return
			}
			// 循环检测：从新 parent 向上走祖先链，遇到自己即循环。
			// （recursive CTE 在 sqlite/PG 双方言兼容，此处用应用层遍历，树深有限。）
			if err := m.checkCycle(r, t.ID, *parent); err != nil {
				httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "parent change would create a cycle"})
				return
			}
		}
		updates["parent_id"] = parent
	}
	if in.AssigneeActorID != nil {
		updates["assignee_actor_id"] = *in.AssigneeActorID
	}

	// 乐观并发：条件更新。0 行 = 并发修改（revision 变了）。
	res := m.DB.WithContext(r.Context()).Model(&model.Task{}).
		Where("id = ? AND revision = ?", t.ID, t.Revision).Updates(updates)
	if res.Error != nil {
		httpx.RespondError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		var current model.Task
		_ = m.DB.First(&current, "id = ?", t.ID).Error
		httpx.WriteError(w, r, &httpx.APIError{
			Status: http.StatusConflict, Code: httpx.CodeRevisionConflict,
			Message: "revision mismatch",
			Details: map[string]any{"current_revision": current.Revision},
		})
		return
	}
	var fresh model.Task
	_ = m.DB.WithContext(r.Context()).First(&fresh, "id = ?", t.ID).Error
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
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
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(fresh, nil))
}

func (m *Module) checkCycle(r *http.Request, taskID, newParent string) error {
	current := newParent
	for depth := 0; depth < 64 && current != ""; depth++ {
		if current == taskID {
			return errors.New("cycle")
		}
		var t model.Task
		err := m.DB.WithContext(r.Context()).Select("parent_id").First(&t, "id = ?", current).Error
		if err != nil || t.ParentID == nil {
			return nil
		}
		current = *t.ParentID
	}
	return nil
}

// Claim 原子认领核心（HTTP handler 与测试共用；roadmap spike 验收项）。
// 单事务内完成：revision 校验 → 抢租约 → 条件更新任务 → audit + outbox。
func (m *Module) Claim(ctx context.Context, p *auth.Principal, taskID string, expectedRevision *int64, leaseSeconds int) (*model.Task, *model.TaskLease, error) {
	var current model.Task
	if err := m.DB.WithContext(ctx).First(&current, "id = ?", taskID).Error; err != nil {
		return nil, nil, &httpx.APIError{Status: 404, Code: httpx.CodeTaskNotFound, Message: "task not found"}
	}
	lease := time.Duration(m.leaseSeconds(leaseSeconds)) * time.Second

	var claimedLease model.TaskLease
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. revision 校验（可选但推荐）。
		if expectedRevision != nil && *expectedRevision != current.Revision {
			return &httpx.APIError{
				Status: http.StatusConflict, Code: httpx.CodeRevisionConflict,
				Message: "revision mismatch",
				Details: map[string]any{"current_revision": current.Revision},
			}
		}
		// 2. 原子抢租约：条件删除旧（过期或自己持有）→ 插入。
		//    并发下第二个事务的 INSERT 会因主键冲突失败 → TASK_ALREADY_CLAIMED。
		//    sqlite 串行写天然原子；PG 靠主键唯一约束兜底。
		res := tx.Where("task_id = ? AND (expires_at < ? OR holder_actor_id = ?)",
			taskID, time.Now(), p.ActorID).Delete(&model.TaskLease{})
		if res.Error != nil {
			return res.Error
		}
		claimedLease = model.TaskLease{
			TaskID:        taskID,
			HolderActorID: p.ActorID,
			ExpiresAt:     time.Now().Add(lease),
			RenewedAt:     time.Now(),
			CreatedAt:     time.Now(),
		}
		if err := tx.Create(&claimedLease).Error; err != nil {
			// 主键冲突 = 别人持有有效租约。
			if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "duplicate key") {
				return &httpx.APIError{
					Status: http.StatusConflict, Code: httpx.CodeTaskAlreadyClaimed,
					Message: "task is already claimed by another actor",
					Details: map[string]any{"task_id": taskID},
				}
			}
			return err
		}
		// 3. 更新任务归属（同样条件更新防并发）。
		updates := map[string]any{
			"assignee_actor_id": p.ActorID,
			"status":            "in_progress",
			"revision":          current.Revision + 1,
			"updated_at":        time.Now(),
			"updated_by":        p.ActorID,
		}
		res = tx.Model(&model.Task{}).Where("id = ? AND revision = ?", taskID, current.Revision).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &httpx.APIError{
				Status: http.StatusConflict, Code: httpx.CodeRevisionConflict,
				Message: "revision mismatch",
				Details: map[string]any{"current_revision": current.Revision},
			}
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
	var fresh model.Task
	_ = m.DB.WithContext(ctx).First(&fresh, "id = ?", taskID).Error
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
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{
		"task":  toTaskDTO(*fresh, claimedLease),
		"lease": toTaskDTO(*fresh, claimedLease).Lease,
	})
}

func (m *Module) renewLease(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	t, apiErr := m.requireTask(r, taskID, auth.ScopeTaskClaim)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		LeaseSeconds int `json:"lease_seconds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	lease := time.Duration(m.leaseSeconds(in.LeaseSeconds)) * time.Second

	now := time.Now()
	var existing model.TaskLease
	err := m.DB.WithContext(r.Context()).First(&existing, "task_id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && existing.ExpiresAt.Before(now)) {
		httpx.WriteError(w, r, &httpx.APIError{Status: 409, Code: httpx.CodeTaskLeaseExpired, Message: "lease expired; claim again"})
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
	_ = t
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
	newRev := t.Revision
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).Delete(&model.TaskLease{}).Error; err != nil {
			return err
		}
		updates := map[string]any{"assignee_actor_id": nil, "revision": t.Revision + 1, "updated_at": time.Now()}
		if t.Status == "in_progress" {
			updates["status"] = "open"
		}
		res := tx.Model(&model.Task{}).Where("id = ? AND revision = ?", taskID, t.Revision).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		newRev = t.Revision + 1
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.release", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskReleased, t.WorkspaceID, p.ActorID, newRev,
			map[string]any{"task_id": taskID})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

// requireWorkspace：workspace 级端点的授权前置（非成员 404 / scope 不足 403）。
func (m *Module) requireWorkspace(r *http.Request, wsID string, need string) *httpx.APIError {
	p := auth.PrincipalFrom(r.Context())
	_, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, wsID, need)
	return apiErr
}

func (m *Module) leaseSeconds(in int) int {
	if in <= 0 {
		return int(m.Auth.LeaseDefault.Seconds())
	}
	if in < int(m.Auth.LeaseMin.Seconds()) {
		return int(m.Auth.LeaseMin.Seconds())
	}
	if in > int(m.Auth.LeaseMax.Seconds()) {
		return int(m.Auth.LeaseMax.Seconds())
	}
	return in
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

type cursorPayload struct {
	C string `json:"c"` // RFC3339Nano
	I string `json:"i"`
}

func encodeCursor(t time.Time, id string) string {
	raw, _ := json.Marshal(cursorPayload{C: t.UTC().Format(time.RFC3339Nano), I: id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", "", err
	}
	var p cursorPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", err
	}
	return p.C, p.I, nil
}

func nextCursorPtr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
