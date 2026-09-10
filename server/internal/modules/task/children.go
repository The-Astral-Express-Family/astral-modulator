// children.go：容器化任务树（TODO.md D15，v2 破坏性重构）。
//
// 统一规则：凡容器（workspace/task），子任务集合是 GET <container>/children、
// 创建是 POST <container>/children——同参数（status/tag/assignee/limit/cursor）、
// 同响应（Task 页，行内批量填充 tags 与 children_count）、同 cursor 分页。
// 根层不是特例：workspace 就是根容器。集合只回传直接子层，不下钻、不平铺；
// 平面查询归 task-search（search.go）。
package task

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
)

// ---- GET <container>/children ----

// listWorkspaceChildren：workspace 容器 = 根层（parent_id IS NULL）。
func (m *Module) listWorkspaceChildren(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	m.listChildren(w, r, wsID, nil)
}

// listTaskChildren：task 容器 = 直接子层。
func (m *Module) listTaskChildren(w http.ResponseWriter, r *http.Request) {
	parent, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	m.listChildren(w, r, parent.WorkspaceID, &parent.ID)
}

// listChildren 容器集合查询核心。parentID == nil 表示 workspace 根层。
// cursor 分页沿用 v1 语义：id 即游标（UUIDv7 字典序 = 创建时间序，
// sqlite/PG 单列比较行为一致）。
func (m *Module) listChildren(w http.ResponseWriter, r *http.Request, wsID string, parentID *string) {
	q := r.URL.Query()
	query := m.DB.WithContext(r.Context()).Model(&model.Task{}).Where("tasks.workspace_id = ?", wsID)
	if parentID == nil {
		query = query.Where("tasks.parent_id IS NULL")
	} else {
		query = query.Where("tasks.parent_id = ?", *parentID)
	}
	// status/tag/assignee 三件套与 task-search 共用同一实现（filters.go）。
	query, apiErr := applyTaskFilters(query, taskFilters{
		Status: q.Get("status"), Tag: q.Get("tag"), Assignee: q.Get("assignee"),
	})
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if v := q.Get("cursor"); v != "" {
		query = query.Where("tasks.id < ?", v)
	}
	limit := parseLimit(q.Get("limit"), 50, 200)

	var rows []model.Task
	if err := query.Order("tasks.id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(m.enrichTasks(r.Context(), rows), next))
}

// ---- POST <container>/children ----

type taskCreateInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Tags        []string `json:"tags"` // 已存在的 tag 规范化名；未知名字 404 整体不创建
}

// createRoot：投递进 workspace 容器（根层任务）。
func (m *Module) createRoot(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskCreateInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	dto, err := m.createTask(r.Context(), p, wsID, nil, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, dto)
}

// createChild：投递进 task 容器（子任务）。
func (m *Module) createChild(w http.ResponseWriter, r *http.Request) {
	parent, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskCreateInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	// 新任务的 parent 不可能构成环（父节点先于子任务存在），无需循环检测。
	dto, err := m.createTask(r.Context(), auth.PrincipalFrom(r.Context()), parent.WorkspaceID, &parent.ID, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, dto)
}

// createTask 创建核心（两个容器端点共用）：校验 → tag 解析 → 单事务
// （任务行 + tag 关联 + audit + outbox）。
func (m *Module) createTask(ctx context.Context, p *auth.Principal, wsID string, parentID *string, in taskCreateInput) (taskDTO, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 500 {
		return taskDTO{}, httpx.Invalid("title required (1-500 chars)")
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !validPriority(in.Priority) {
		return taskDTO{}, httpx.Invalid("invalid priority")
	}
	if parentID != nil {
		var parent model.Task
		err := m.DB.WithContext(ctx).First(&parent, "id = ?", *parentID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && parent.WorkspaceID != wsID) {
			return taskDTO{}, httpx.Invalid("parent must exist in the same workspace")
		}
		if err != nil {
			return taskDTO{}, err
		}
	}
	tagIDs, err := m.resolveTagNames(ctx, wsID, in.Tags)
	if err != nil {
		return taskDTO{}, err
	}

	t := model.Task{
		ID: ids.New(ids.Task), WorkspaceID: wsID, ParentID: parentID,
		Title: in.Title, Description: in.Description,
		Status: "open", Priority: in.Priority, Revision: 1,
		CreatedBy: p.ActorID,
	}
	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		for _, tagID := range tagIDs {
			if err := tx.Create(&model.TaskTag{TaskID: t.ID, TagID: tagID, AddedBy: p.ActorID}).Error; err != nil {
				return err
			}
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
		return taskDTO{}, err
	}
	return toTaskDTO(t, nil, m.loadTaskTags(ctx, t.ID), 0), nil
}

// resolveTagNames 按规范化名解析 workspace 内既有 tag；未知名字 → 404
// （整体不创建，避免静默丢弃调用者意图）。
func (m *Module) resolveTagNames(ctx context.Context, wsID string, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	idsOut := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		norm := tag.NormalizeName(name)
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		var row model.Tag
		err := m.DB.WithContext(ctx).First(&row, "workspace_id = ? AND normalized_name = ?", wsID, norm).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, httpx.NotFound("tag '" + name + "' does not exist in this workspace")
		}
		if err != nil {
			return nil, err
		}
		idsOut = append(idsOut, row.ID)
	}
	return idsOut, nil
}

// ---- 批量填充（tags + children_count；D15 修订 D11）----

// enrichTasks 集合行组装（children 集合与 task-search 两处共用）：每页各一次
// 批量查询，杜绝 N+1。
func (m *Module) enrichTasks(ctx context.Context, rows []model.Task) []taskDTO {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	tagsByTask := m.tagsForTasks(ctx, ids)
	counts := m.childCounts(ctx, ids)
	items := make([]taskDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toTaskDTO(rows[i], nil, tagsByTask[rows[i].ID], counts[rows[i].ID]))
	}
	return items
}

func (m *Module) tagsForTasks(ctx context.Context, taskIDs []string) map[string][]tag.TagDTO {
	out := make(map[string][]tag.TagDTO, len(taskIDs))
	if len(taskIDs) == 0 {
		return out
	}
	var rows []struct {
		TaskID      string
		ID          string
		WorkspaceID string
		Name        string
	}
	err := m.DB.WithContext(ctx).
		Table("task_tags").
		Select("task_tags.task_id AS task_id, tags.id AS id, tags.workspace_id AS workspace_id, tags.name AS name").
		Joins("JOIN tags ON tags.id = task_tags.tag_id").
		Where("task_tags.task_id IN ?", taskIDs).
		Order("tags.created_at ASC").
		Scan(&rows).Error
	if err != nil {
		m.logger().Warn("batch load task tags failed", "err", err)
		return out
	}
	for _, row := range rows {
		out[row.TaskID] = append(out[row.TaskID], tag.TagDTO{ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name})
	}
	return out
}

func (m *Module) childCounts(ctx context.Context, parentIDs []string) map[string]int64 {
	out := make(map[string]int64, len(parentIDs))
	if len(parentIDs) == 0 {
		return out
	}
	var rows []struct {
		ParentID string
		Cnt      int64
	}
	err := m.DB.WithContext(ctx).Model(&model.Task{}).
		Select("parent_id, COUNT(*) AS cnt").
		Where("parent_id IN ?", parentIDs).
		Group("parent_id").
		Scan(&rows).Error
	if err != nil {
		m.logger().Warn("batch child count failed", "err", err)
		return out
	}
	for _, row := range rows {
		out[row.ParentID] = row.Cnt
	}
	return out
}

// childCount 单任务直接子任务数（变更响应恒填充 children_count 用）。
// 批量版 childCounts 的退化调用——计数只有一处实现。
func (m *Module) childCount(ctx context.Context, taskID string) int64 {
	return m.childCounts(ctx, []string{taskID})[taskID]
}
