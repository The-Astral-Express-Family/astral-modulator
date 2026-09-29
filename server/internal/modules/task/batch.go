// batch.go：任务批量管理（协议 2.1 task_batch，TODO.md §9 2026-09-30）。
//
// 三个批量端点共用纪律：
//   - 整批单事务、全有或全无（与 children 创建"tags 未知名字整体不创建"
//     同一口味）：客户端重试语义 = 整批重放，不产生半批状态；
//   - 领域事件逐任务 emit 既有词表（task.created / task.updated），不新增
//     事件类型（SSE 消费端零改动，三方同步门不动）；
//   - audit 每批一条（details 带摘要），TargetID 记首个受影响任务。
//
// 批量上限：树 ≤200 节点 / 深度 ≤8（checkCycle 祖先链帽 64 之上的防灌深
// 闸），move / batch-update ≤200 项（与集合 limit 上限同口径）。
package task

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

const (
	maxTreeRoots = 50
	maxTreeNodes = 200
	maxTreeDepth = 8
	maxBatchOps  = 200
)

// ---- 批量树形创建：POST <container>/task-trees ----

type taskTreeNodeIn struct {
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Priority    string           `json:"priority"`
	Tags        []string         `json:"tags"`
	Children    []taskTreeNodeIn `json:"children"`
}

type taskTreesIn struct {
	Trees []taskTreeNodeIn `json:"trees"`
}

type taskTreeNodeDTO struct {
	Task     taskDTO           `json:"task"`
	Children []taskTreeNodeDTO `json:"children"`
}

type taskTreesOut struct {
	Items []taskTreeNodeDTO `json:"items"`
}

// createRootTrees：向 workspace 容器投递 1..N 棵根树。
func (m *Module) createRootTrees(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskTreesIn
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	out, err := m.CreateTaskTrees(r.Context(), auth.PrincipalFrom(r.Context()), wsID, nil, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, out)
}

// createChildTrees：向 task 容器投递 1..N 棵子树（parent=容器任务）。
func (m *Module) createChildTrees(w http.ResponseWriter, r *http.Request) {
	parent, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskTreesIn
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	out, err := m.CreateTaskTrees(r.Context(), auth.PrincipalFrom(r.Context()), parent.WorkspaceID, &parent.ID, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, out)
}

// flatNode 是先序展平后的节点：parentIdx 指向展平序列中的父节点（-1 = 挂容器）。
type flatNode struct {
	depth     int
	parentIdx int
	in        *taskTreeNodeIn
	tagIDs    []string
}

// flattenTrees 先序展平并做形状校验（根树数/节点数/深度/title/priority）。
// 展平序即创建序与 details 定位序（与请求先序一致）。
func flattenTrees(trees []taskTreeNodeIn) ([]flatNode, *httpx.APIError) {
	if len(trees) == 0 || len(trees) > maxTreeRoots {
		return nil, batchInvalid("trees must contain 1-"+strconv.Itoa(maxTreeRoots)+" root trees", nil)
	}
	flat := make([]flatNode, 0, maxTreeNodes)
	var walk func(nodes []taskTreeNodeIn, depth, parentIdx int) *httpx.APIError
	walk = func(nodes []taskTreeNodeIn, depth, parentIdx int) *httpx.APIError {
		for i := range nodes {
			if len(flat) >= maxTreeNodes {
				return batchInvalid("batch exceeds "+strconv.Itoa(maxTreeNodes)+" nodes",
					map[string]any{"limit": maxTreeNodes})
			}
			n := &nodes[i]
			title := strings.TrimSpace(n.Title)
			if title == "" || len(title) > 500 {
				return batchInvalid("title required (1-500 bytes)", map[string]any{"field": "title"})
			}
			priority := n.Priority
			if priority == "" {
				priority = "normal"
			}
			if !validPriority(priority) {
				return batchInvalid("invalid priority", map[string]any{"field": "priority"})
			}
			n.Title = title
			n.Priority = priority
			idx := len(flat)
			flat = append(flat, flatNode{depth: depth, parentIdx: parentIdx, in: n})
			if len(n.Children) > 0 {
				if depth+1 > maxTreeDepth {
					return batchInvalid("batch exceeds max tree depth "+strconv.Itoa(maxTreeDepth),
						map[string]any{"limit": maxTreeDepth})
				}
				if err := walk(n.Children, depth+1, idx); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(trees, 1, -1); err != nil {
		return nil, err
	}
	return flat, nil
}

// CreateTaskTrees 批量树形创建核心（HTTP handler 与测试共用，与 createTask
// 同规矩：授权在 handler，本函数只做校验与事务）。容器 parentID==nil 表示
// workspace 根层。整批单事务：任一节点非法 → 400/404 且零行落库。
func (m *Module) CreateTaskTrees(ctx context.Context, p *auth.Principal, wsID string, containerParent *string, in taskTreesIn) (taskTreesOut, error) {
	flat, apiErr := flattenTrees(in.Trees)
	if apiErr != nil {
		return taskTreesOut{}, apiErr
	}
	// 全部节点的 tag 名字汇总一次解析（规范化名→tag id），未知名字 404
	// 整批不创建——与单创建 resolveTagNames 同语义，避免静默丢弃意图。
	tagMap, apiErr := m.resolveTreeTags(ctx, wsID, flat)
	if apiErr != nil {
		return taskTreesOut{}, apiErr
	}
	for i := range flat {
		flat[i].tagIDs = collectTagIDs(flat[i].in.Tags, tagMap)
	}

	created := make([]model.Task, len(flat))
	// 容器层序位起点在事务外计数（sqlite deferred 事务首条必须是写，见
	// createTask 同款注释）；批内新建父任务的儿子从 0 起按请求先序递增。
	var containerNext int64
	if err := siblingScope(m.DB.WithContext(ctx), wsID, containerParent).Count(&containerNext).Error; err != nil {
		return taskTreesOut{}, err
	}
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		nextPos := map[string]int64{}
		for i := range flat {
			fn := &flat[i]
			var parentID *string
			var pos int64
			if fn.parentIdx >= 0 {
				parentID = &created[fn.parentIdx].ID
				pos = nextPos[*parentID]
				nextPos[*parentID] = pos + 1
			} else {
				parentID = containerParent
				pos = containerNext
				containerNext++
			}
			t := model.Task{
				ID: ids.New(ids.Task), WorkspaceID: wsID, ParentID: parentID,
				Title: fn.in.Title, Description: fn.in.Description,
				Status: "open", Priority: fn.in.Priority, Revision: 1,
				Position: pos, CreatedBy: p.ActorID,
			}
			if err := tx.Create(&t).Error; err != nil {
				return err
			}
			for _, tagID := range fn.tagIDs {
				if err := tx.Create(&model.TaskTag{TaskID: t.ID, TagID: tagID, AddedBy: p.ActorID}).Error; err != nil {
					return err
				}
			}
			if err := outbox.EmitTx(tx, outbox.TypeTaskCreated, wsID, p.ActorID, 1,
				map[string]any{"task_id": t.ID, "title": t.Title}); err != nil {
				return err
			}
			created[i] = t
		}
		rootIDs := make([]string, 0, len(in.Trees))
		for i := range flat {
			if flat[i].parentIdx < 0 {
				rootIDs = append(rootIDs, created[i].ID)
			}
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "task.batch_create", Outcome: "allowed",
			TargetType: "task", TargetID: rootIDs[0],
			Details: map[string]any{"count": len(flat), "root_ids": rootIDs},
		})
	})
	if err != nil {
		return taskTreesOut{}, err
	}
	return taskTreesOut{Items: linkTrees(in.Trees, m.enrichFlat(ctx, created))}, nil
}

// enrichFlat 提交后批量填充（tags + children_count），返回与展平序同序的 DTO。
func (m *Module) enrichFlat(ctx context.Context, created []model.Task) []taskTreeNodeDTO {
	all := make([]string, 0, len(created))
	for i := range created {
		all = append(all, created[i].ID)
	}
	tagsByTask := m.tagsForTasks(ctx, all)
	counts := m.childCounts(ctx, all)
	views := m.depViewsForTasks(ctx, all)
	dtos := make([]taskTreeNodeDTO, len(created))
	for i := range created {
		dtos[i] = taskTreeNodeDTO{
			Task:     toTaskDTO(created[i], tagsByTask[created[i].ID], counts[created[i].ID], views[created[i].ID]),
			Children: []taskTreeNodeDTO{},
		}
	}
	return dtos
}

// linkTrees 按请求树形把展平 DTO 序列挂回嵌套结构（flatten 的逆变换）。
func linkTrees(trees []taskTreeNodeIn, dtos []taskTreeNodeDTO) []taskTreeNodeDTO {
	pos := 0
	var rec func(nodes []taskTreeNodeIn) []taskTreeNodeDTO
	rec = func(nodes []taskTreeNodeIn) []taskTreeNodeDTO {
		out := make([]taskTreeNodeDTO, 0, len(nodes))
		for i := range nodes {
			node := dtos[pos]
			pos++
			node.Children = rec(nodes[i].Children)
			out = append(out, node)
		}
		return out
	}
	return rec(trees)
}

// resolveTreeTags 汇总全部节点的 tag 名字一次解析（规范化名→tag id）；
// 未知名字 404（文案与单创建一致）。
func (m *Module) resolveTreeTags(ctx context.Context, wsID string, flat []flatNode) (map[string]string, *httpx.APIError) {
	out := map[string]string{}
	for i := range flat {
		for _, name := range flat[i].in.Tags {
			norm := tag.NormalizeName(name)
			if norm == "" {
				continue
			}
			if _, dup := out[norm]; dup {
				continue
			}
			var row model.Tag
			if apiErr := store.First(m.DB.WithContext(ctx), &row,
				httpx.NotFound("tag '"+name+"' does not exist in this workspace"),
				"workspace_id = ? AND normalized_name = ?", wsID, norm); apiErr != nil {
				return nil, apiErr
			}
			out[norm] = row.ID
		}
	}
	return out, nil
}

// collectTagIDs 把节点 tag 名字映射为 tag id（空规范化名与重复跳过，与
// 单创建 resolveTagNames 的去重口径一致）。
func collectTagIDs(names []string, tagMap map[string]string) []string {
	if len(names) == 0 {
		return nil
	}
	idsOut := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		norm := tag.NormalizeName(name)
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		idsOut = append(idsOut, tagMap[norm])
	}
	return idsOut
}

// ---- 批量移动：POST /workspaces/{id}/tasks/move ----

type taskMoveItemIn struct {
	TaskID           string  `json:"task_id"`
	ParentID         *string `json:"parent_id"` // null = 移到根层
	ExpectedRevision int64   `json:"expected_revision"`
	Position         *int    `json:"position,omitempty"` // 目标兄弟序位（0 起）；缺省 = 追加末尾
}

type taskMoveIn struct {
	Items []taskMoveItemIn `json:"items"`
}

type taskBatchOut struct {
	Items []taskDTO `json:"items"`
}

func (m *Module) moveTasks(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskMoveIn
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	out, err := m.MoveTasks(r.Context(), auth.PrincipalFrom(r.Context()), wsID, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, out)
}

// MoveTasks 批量移动核心。语义：parent_id=null 移到根层；不支持跨 workspace
// （不属于该 ws 的 task_id 视为不存在，parent 跨 ws 走 validateParent 同文案）；
// 子孙随 parent 语义自然跟随；被移动任务的租约不随父变化（租约挂在任务上）。
// 校验全批通过后才落库：revision 冲突/环/跨 ws 任一命中 → 整批不生效。
// 批内互移参与统一环检测（override 视图模拟最终 parent 状态）。
// position（2.4）：同一次调用完成换父与兄弟内重排——插入到目标列表摘除前
// 下标处（同父且原位在下标之前则等效下标 -1），越界收敛到末尾；缺省追加。
// items 按请求顺序生效；兄弟位移不 bump 兄弟 revision，仅被移动任务自身 bump。
func (m *Module) MoveTasks(ctx context.Context, p *auth.Principal, wsID string, in taskMoveIn) (taskBatchOut, error) {
	order, byID, err := m.preloadBatch(ctx, wsID, batchItems(in.Items))
	if err != nil {
		return taskBatchOut{}, err
	}
	for _, it := range in.Items {
		if it.ExpectedRevision != byID[it.TaskID].Revision {
			return taskBatchOut{}, batchRevisionConflict(byID[it.TaskID].Revision, it.TaskID)
		}
		if it.Position != nil && *it.Position < 0 {
			return taskBatchOut{}, batchInvalid("position must be >= 0", map[string]any{"task_id": it.TaskID})
		}
	}

	// 批内 parent 生效视图：override[id] = 该任务批内新 parent（nil=根层）。
	override := map[string]*string{}
	for _, it := range in.Items {
		override[it.TaskID] = it.ParentID
	}
	// parent 存在性预检：不在批内的 parent 必须存在于同 workspace
	// （与 validateParent 同文案）；在批内的天然满足。
	for _, it := range in.Items {
		if it.ParentID == nil {
			continue
		}
		if _, inBatch := override[*it.ParentID]; inBatch {
			continue
		}
		var parent model.Task
		if apiErr := store.First(m.DB.WithContext(ctx), &parent,
			httpx.Invalid("parent must exist in the same workspace"), "id = ?", *it.ParentID); apiErr != nil {
			return taskBatchOut{}, apiErr
		}
		if parent.WorkspaceID != wsID {
			return taskBatchOut{}, httpx.Invalid("parent must exist in the same workspace")
		}
	}
	// 环检测（批内感知）：从新 parent 沿最终 parent 状态向上走，遇到自己即环。
	for _, it := range in.Items {
		if it.ParentID == nil {
			continue
		}
		cyclic, err := m.cycleInBatch(ctx, override, it.TaskID, *it.ParentID)
		if err != nil {
			return taskBatchOut{}, err
		}
		if cyclic {
			return taskBatchOut{}, batchInvalid("parent change would create a cycle",
				map[string]any{"task_id": it.TaskID, "parent_id": *it.ParentID})
		}
	}

	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, it := range in.Items {
			// 批内按序生效：前序项的兄弟位移会改变行状态，后续项须事务内现读。
			// 首项直接用预载行——事务首条语句必须是写（sqlite deferred 事务
			// 「先读后写」升锁无视 busy_timeout 直接 SQLITE_BUSY）。
			cur := byID[it.TaskID]
			if i > 0 {
				if err := tx.First(&cur, "id = ?", it.TaskID).Error; err != nil {
					return err
				}
			}
			sameParent := (it.ParentID == nil && cur.ParentID == nil) ||
				(it.ParentID != nil && cur.ParentID != nil && *it.ParentID == *cur.ParentID)
			// 1) 摘除：老兄弟列表补洞（原位之后的兄弟前移一位）。
			if err := siblingScope(tx, wsID, cur.ParentID).
				Where("id <> ? AND position > ?", cur.ID, cur.Position).
				Update("position", gorm.Expr("position - 1")).Error; err != nil {
				return err
			}
			// 2) 目标插入下标：缺省 = 当前兄弟数（末尾）；显式 position 以摘除前
			//    列表为准（同父且原位在下标之前 → 摘除后等效下标 -1），越界收敛。
			var cnt int64
			if err := siblingScope(tx, wsID, it.ParentID).Count(&cnt).Error; err != nil {
				return err
			}
			idx := cnt
			if it.Position != nil {
				idx = int64(*it.Position)
				if sameParent && int64(*it.Position) > cur.Position {
					idx--
				}
				if idx < 0 {
					idx = 0
				}
				if idx > cnt {
					idx = cnt
				}
			}
			// 3) 让位：目标列表 position >= idx 的兄弟后移一位（含仍在老列表里的
			//    被移动任务自身，落位时覆写）。
			if err := siblingScope(tx, wsID, it.ParentID).
				Where("position >= ?", idx).
				Update("position", gorm.Expr("position + 1")).Error; err != nil {
				return err
			}
			// 4) 落位 + revision bump（乐观并发单点 bumpRevisionTx）。
			if err := bumpRevisionTx(tx, it.TaskID, it.ExpectedRevision, map[string]any{
				"parent_id":  it.ParentID,
				"position":   idx,
				"updated_by": p.ActorID,
			}); err != nil {
				return err
			}
			if err := outbox.EmitTx(tx, outbox.TypeTaskUpdated, wsID, p.ActorID, it.ExpectedRevision+1,
				map[string]any{"task_id": it.TaskID}); err != nil {
				return err
			}
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "task.batch_move", Outcome: "allowed",
			TargetType: "task", TargetID: order[0],
			Details: map[string]any{"count": len(order), "task_ids": order},
		})
	})
	if err != nil {
		return taskBatchOut{}, err
	}
	return m.reloadBatch(ctx, order), nil
}

// cycleInBatch 沿"批内最终 parent 状态"向上遍历（override 优先，批外任务
// 读库），帽 64 与 checkCycle 一致。返回是否形成环。
func (m *Module) cycleInBatch(ctx context.Context, override map[string]*string, taskID, newParent string) (bool, error) {
	current := newParent
	for depth := 0; depth < 64 && current != ""; depth++ {
		if current == taskID {
			return true, nil
		}
		if p, ok := override[current]; ok {
			if p == nil {
				return false, nil
			}
			current = *p
			continue
		}
		var t model.Task
		err := m.DB.WithContext(ctx).Select("parent_id").First(&t, "id = ?", current).Error
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

// ---- 批量同值更新：POST /workspaces/{id}/tasks/batch-update ----

type taskBatchUpdateItemIn struct {
	TaskID           string `json:"task_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type taskBatchUpdateIn struct {
	Items []taskBatchUpdateItemIn `json:"items"`
	Set   struct {
		Status          *string  `json:"status"`
		Priority        *string  `json:"priority"`
		AssigneeActorID **string `json:"assignee_actor_id"` // 三态：缺省不改，null 清空
	} `json:"set"`
}

func (m *Module) batchUpdate(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTaskWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in taskBatchUpdateIn
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	out, err := m.BatchUpdateTasks(r.Context(), auth.PrincipalFrom(r.Context()), wsID, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, out)
}

// BatchUpdateTasks 批量同值更新核心：对选中集合应用同一组字段（至少一项）。
// 典型用途 = 批量完成/批量取消（T1：取消走 status=cancelled，无 DELETE）。
// 全有或全无，校验纪律与 MoveTasks 相同。
func (m *Module) BatchUpdateTasks(ctx context.Context, p *auth.Principal, wsID string, in taskBatchUpdateIn) (taskBatchOut, error) {
	if len(in.Items) == 0 || len(in.Items) > maxBatchOps {
		return taskBatchOut{}, batchInvalid("items must contain 1-"+strconv.Itoa(maxBatchOps)+" entries", nil)
	}
	if in.Set.Status == nil && in.Set.Priority == nil && in.Set.AssigneeActorID == nil {
		return taskBatchOut{}, batchInvalid("set must contain at least one field", map[string]any{"field": "set"})
	}
	if in.Set.Status != nil && !validStatus(*in.Set.Status) {
		return taskBatchOut{}, batchInvalid("invalid status", map[string]any{"field": "status"})
	}
	if in.Set.Priority != nil && !validPriority(*in.Set.Priority) {
		return taskBatchOut{}, batchInvalid("invalid priority", map[string]any{"field": "priority"})
	}

	order, byID, err := m.preloadBatch(ctx, wsID, batchItemIDs(in))
	if err != nil {
		return taskBatchOut{}, err
	}
	for _, it := range in.Items {
		if it.ExpectedRevision != byID[it.TaskID].Revision {
			return taskBatchOut{}, batchRevisionConflict(byID[it.TaskID].Revision, it.TaskID)
		}
	}

	updates := map[string]any{"updated_by": p.ActorID}
	if in.Set.Status != nil {
		updates["status"] = *in.Set.Status
	}
	if in.Set.Priority != nil {
		updates["priority"] = *in.Set.Priority
	}
	if in.Set.AssigneeActorID != nil {
		updates["assignee_actor_id"] = *in.Set.AssigneeActorID
	}

	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, it := range in.Items {
			if err := bumpRevisionTx(tx, it.TaskID, it.ExpectedRevision, updates); err != nil {
				return err
			}
			if err := outbox.EmitTx(tx, outbox.TypeTaskUpdated, wsID, p.ActorID, it.ExpectedRevision+1,
				map[string]any{"task_id": it.TaskID}); err != nil {
				return err
			}
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "task.batch_update", Outcome: "allowed",
			TargetType: "task", TargetID: order[0],
			Details: map[string]any{"count": len(order), "task_ids": order},
		})
	})
	if err != nil {
		return taskBatchOut{}, err
	}
	return m.reloadBatch(ctx, order), nil
}

// ---- 共用小件 ----

func batchItems(items []taskMoveItemIn) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.TaskID)
	}
	return out
}

func batchItemIDs(in taskBatchUpdateIn) []string {
	out := make([]string, 0, len(in.Items))
	for _, it := range in.Items {
		out = append(out, it.TaskID)
	}
	return out
}

// preloadBatch 批量端点的公共前半场：数量上限 → 重复 task_id → 存在性 +
// 同 workspace。返回请求顺序的 id 列表与行映射。任一命中 → 整批错误。
func (m *Module) preloadBatch(ctx context.Context, wsID string, taskIDs []string) ([]string, map[string]model.Task, error) {
	if len(taskIDs) == 0 || len(taskIDs) > maxBatchOps {
		return nil, nil, batchInvalid("items must contain 1-"+strconv.Itoa(maxBatchOps)+" entries", nil)
	}
	seen := map[string]struct{}{}
	for _, id := range taskIDs {
		if _, dup := seen[id]; dup {
			return nil, nil, batchInvalid("duplicate task_id in batch", map[string]any{"task_id": id})
		}
		seen[id] = struct{}{}
	}
	rows := make([]model.Task, 0, len(taskIDs))
	if err := m.DB.WithContext(ctx).Where("id IN ?", taskIDs).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[string]model.Task, len(rows))
	for _, t := range rows {
		byID[t.ID] = t
	}
	for _, id := range taskIDs {
		t, ok := byID[id]
		if !ok || t.WorkspaceID != wsID {
			return nil, nil, &httpx.APIError{
				Status: http.StatusNotFound, Code: httpx.CodeTaskNotFound,
				Message: "task not found", Details: map[string]any{"task_id": id},
			}
		}
	}
	return taskIDs, byID, nil
}

// batchRevisionConflict 批量端点的 409（details 附 task_id 定位冲突项，
// current_revision 供 re-read-retry，与单任务 revisionConflict 同码）。
func batchRevisionConflict(current int64, taskID string) *httpx.APIError {
	return &httpx.APIError{
		Status: http.StatusConflict, Code: httpx.CodeRevisionConflict,
		Message: "revision mismatch",
		Details: map[string]any{"current_revision": current, "task_id": taskID},
	}
}

// reloadBatch 提交后按请求顺序重读并组装（批量填充 tags/children_count）。
func (m *Module) reloadBatch(ctx context.Context, order []string) taskBatchOut {
	var rows []model.Task
	if err := m.DB.WithContext(ctx).Where("id IN ?", order).Find(&rows).Error; err != nil {
		m.logger().Warn("batch reload failed", "err", err)
		return taskBatchOut{Items: []taskDTO{}}
	}
	enriched := m.enrichTasks(ctx, rows)
	byID := make(map[string]taskDTO, len(enriched))
	for _, dto := range enriched {
		byID[dto.ID] = dto
	}
	out := make([]taskDTO, 0, len(order))
	for _, id := range order {
		if dto, ok := byID[id]; ok {
			out = append(out, dto)
		}
	}
	return taskBatchOut{Items: out}
}

// batchInvalid 批量端点的 400 构造（details 携带定位信息）。
func batchInvalid(message string, details map[string]any) *httpx.APIError {
	return &httpx.APIError{
		Status: http.StatusBadRequest, Code: httpx.CodeValidationFailed,
		Message: message, Details: details,
	}
}
