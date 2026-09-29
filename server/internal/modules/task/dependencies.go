// dependencies.go：任务依赖边（协议 2.2 task_dependencies，TODO.md §9 2026-09-30）。
//
// 方向约定：from = 依赖方（等待者），to = 被依赖方（blocker）——
// PUT /tasks/{a}/dependencies/{b} 读作「a 依赖 b」。kind=blocks 为硬阻塞
// （写入时沿 blocks 边防环），kind=relates 为对称关联（无方向语义，不参与
// 环检测）。边不是任务事实本身，但改变两端任务的展示视图（blocked_by /
// blocks / related），故写路径按 D11 tag 语义处理：两端各 bump revision 并
// 各发 task.updated（data.dep_change），audit 每次边操作一条。
//
// 依赖目前只做表达/展示/过滤，不拦截 claim/done（登记 TODO.md §3 触发式
// 延期：出现误用场景再议硬拦截）。
package task

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// 单任务单方向的依赖边上限（blocks 与 relates 各自计数，防灌边）。
const maxDepsPerKind = 50

func validDependencyKind(kind string) bool {
	return kind == "blocks" || kind == "relates"
}

type dependencyEdgeDTO struct {
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
	Kind       string `json:"kind"`
	CreatedAt  string `json:"created_at"`
}

type dependencyListOut struct {
	Items []dependencyEdgeDTO `json:"items"`
}

// ---- GET /tasks/{id}/dependencies ----

func (m *Module) listDependencies(w http.ResponseWriter, r *http.Request) {
	t, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var edges []model.TaskDependency
	if err := m.DB.WithContext(r.Context()).
		Where("from_task_id = ? OR to_task_id = ?", t.ID, t.ID).
		Order("created_at ASC, from_task_id ASC, to_task_id ASC, kind ASC").
		Find(&edges).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]dependencyEdgeDTO, 0, len(edges))
	for _, e := range edges {
		items = append(items, dependencyEdgeDTO{
			FromTaskID: e.FromTaskID, ToTaskID: e.ToTaskID, Kind: e.Kind,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, dependencyListOut{Items: items})
}

// ---- PUT /tasks/{id}/dependencies/{dependency_task_id} ----

func (m *Module) addDependency(w http.ResponseWriter, r *http.Request) {
	a, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		Kind string `json:"kind"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Kind == "" {
		in.Kind = "blocks"
	}
	if !validDependencyKind(in.Kind) {
		httpx.WriteError(w, r, httpx.Invalid("kind must be blocks or relates"))
		return
	}
	// 依赖关系限于同 workspace；跨 ws 的 task_id 按不存在处理（不泄露存在性，
	// 与 LoadForWorkspace 的 404 语义一致）。
	b, apiErr := m.requireTask(r, chi.URLParam(r, "dependency_task_id"), auth.ScopeTaskRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	created, err := m.AddDependency(r.Context(), auth.PrincipalFrom(r.Context()), a, b, in.Kind)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK // 幂等 PUT：已存在同边不 bump（与 tag attach 同规矩）
	}
	httpx.WriteOK(w, r, status, map[string]any{
		"from_task_id": a.ID, "to_task_id": b.ID, "kind": in.Kind,
	})
}

// AddDependency 依赖边写入核心（handler 与测试共用）。校验全通过后单事务：
// 边行 + 两端条件 revision bump + 两端 task.updated（dep_change）+ audit 一条。
// 返回是否新建（已存在同边为幂等 no-op → false）。
func (m *Module) AddDependency(ctx context.Context, p *auth.Principal, a, b *model.Task, kind string) (bool, error) {
	if a.ID == b.ID {
		return false, httpx.Invalid("a task cannot depend on itself")
	}
	if a.WorkspaceID != b.WorkspaceID {
		return false, httpx.Invalid("dependency target must be in the same workspace")
	}
	var cnt int64
	if err := m.DB.WithContext(ctx).Model(&model.TaskDependency{}).
		Where("from_task_id = ? AND kind = ?", a.ID, kind).Count(&cnt).Error; err != nil {
		return false, err
	}
	if cnt >= maxDepsPerKind {
		return false, httpx.Invalid("too many dependencies (" + strconv.Itoa(maxDepsPerKind) + " max per kind)")
	}
	if kind == "blocks" {
		cyclic, err := m.depCycle(ctx, a.ID, b.ID)
		if err != nil {
			return false, err
		}
		if cyclic {
			return false, httpx.Invalid("dependency would create a cycle")
		}
	}
	var existing model.TaskDependency
	err := m.DB.WithContext(ctx).First(&existing,
		"from_task_id = ? AND to_task_id = ? AND kind = ?", a.ID, b.ID, kind).Error
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	edge := model.TaskDependency{FromTaskID: a.ID, ToTaskID: b.ID, Kind: kind, CreatedBy: p.ActorID}
	// 两端 bump 按 id 字典序固定先后，避免相反方向的并发插入在 PG 下死锁。
	aChange := map[string]any{"op": "add", "kind": kind, "peer": b.ID, "role": "from"}
	bChange := map[string]any{"op": "add", "kind": kind, "peer": a.ID, "role": "to"}
	pair := [][2]any{{a, aChange}, {b, bChange}}
	if a.ID > b.ID {
		pair[0], pair[1] = pair[1], pair[0]
	}
	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&edge).Error; err != nil {
			return err
		}
		for _, side := range pair {
			if err := m.bumpForDepChange(tx, side[0].(*model.Task), p, side[1].(map[string]any)); err != nil {
				return err
			}
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: a.WorkspaceID, ActorID: p.ActorID,
			Action: "task.dependency.add", Outcome: "allowed",
			TargetType: "task", TargetID: a.ID,
			Details: map[string]any{"kind": kind, "peer": b.ID},
		})
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// bumpForDepChange 单端任务的条件 revision bump + task.updated（dep_change）。
// 条件更新 0 行（并发修改）→ REVISION_CONFLICT，整事务回滚。
func (m *Module) bumpForDepChange(tx *gorm.DB, t *model.Task, p *auth.Principal, depChange map[string]any) error {
	if err := bumpRevisionTx(tx, t.ID, t.Revision, map[string]any{"updated_by": p.ActorID}); err != nil {
		return err
	}
	return outbox.EmitTx(tx, outbox.TypeTaskUpdated, t.WorkspaceID, p.ActorID, t.Revision+1,
		map[string]any{"task_id": t.ID, "dep_change": depChange})
}

// depCycle 沿 blocks 依赖边（from → to）从 b 遍历全部依赖分支，回到 a 即
// 成环：加入 a→b 后链为 a ⇒ b ⇒ … ⇒ a。帽 64 与 checkCycle 一致。
func (m *Module) depCycle(ctx context.Context, a, b string) (bool, error) {
	frontier := []string{b}
	for depth := 0; depth < 64 && len(frontier) > 0; depth++ {
		var nexts []string
		for _, current := range frontier {
			if current == a {
				return true, nil
			}
			var rows []model.TaskDependency
			if err := m.DB.WithContext(ctx).
				Select("to_task_id").
				Where("from_task_id = ? AND kind = ?", current, "blocks").
				Find(&rows).Error; err != nil {
				return false, err
			}
			for _, row := range rows {
				nexts = append(nexts, row.ToTaskID)
			}
		}
		frontier = nexts
	}
	return false, nil
}

// ---- DELETE /tasks/{id}/dependencies/{dependency_task_id}?kind= ----

func (m *Module) removeDependency(w http.ResponseWriter, r *http.Request) {
	a, apiErr := m.requireTask(r, chi.URLParam(r, "task_id"), auth.ScopeTaskWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "blocks"
	}
	if !validDependencyKind(kind) {
		httpx.WriteError(w, r, httpx.Invalid("kind must be blocks or relates"))
		return
	}
	b, apiErr := m.requireTask(r, chi.URLParam(r, "dependency_task_id"), auth.ScopeTaskRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if err := m.RemoveDependency(r.Context(), auth.PrincipalFrom(r.Context()), a, b, kind); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// 幂等删除：边不存在也 204（与 tag detach 同语义）。
	w.WriteHeader(http.StatusNoContent)
}

// RemoveDependency 依赖边删除核心：边不存在 → 幂等 no-op；存在 → 单事务
// 删行 + 两端 bump + task.updated（op=remove）+ audit 一条。
func (m *Module) RemoveDependency(ctx context.Context, p *auth.Principal, a, b *model.Task, kind string) error {
	res := m.DB.WithContext(ctx).Where(
		"from_task_id = ? AND to_task_id = ? AND kind = ?", a.ID, b.ID, kind).
		Delete(&model.TaskDependency{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}
	aChange := map[string]any{"op": "remove", "kind": kind, "peer": b.ID, "role": "from"}
	bChange := map[string]any{"op": "remove", "kind": kind, "peer": a.ID, "role": "to"}
	pair := [][2]any{{a, aChange}, {b, bChange}}
	if a.ID > b.ID {
		pair[0], pair[1] = pair[1], pair[0]
	}
	return m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, side := range pair {
			if err := m.bumpForDepChange(tx, side[0].(*model.Task), p, side[1].(map[string]any)); err != nil {
				return err
			}
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: a.WorkspaceID, ActorID: p.ActorID,
			Action: "task.dependency.remove", Outcome: "allowed",
			TargetType: "task", TargetID: a.ID,
			Details: map[string]any{"kind": kind, "peer": b.ID},
		})
	})
}

// ---- 视图批量填充（Task.blocked_by / blocks / related）----

type depViews struct {
	blockedBy []string
	blocks    []string
	related   []string
}

// depViewsForTasks 一次查询装填全部视图集合（children/search enrich 与
// 单任务详情共用；杜绝 N+1）。blocked_by/blocks 只含 blocks 语义边，
// related 为 relates 边的对端；空集合返回空切片（DTO 恒填充惯例）。
func (m *Module) depViewsForTasks(ctx context.Context, taskIDs []string) map[string]depViews {
	out := make(map[string]depViews, len(taskIDs))
	if len(taskIDs) == 0 {
		return out
	}
	for _, id := range taskIDs {
		out[id] = depViews{blockedBy: []string{}, blocks: []string{}, related: []string{}}
	}
	var edges []model.TaskDependency
	if err := m.DB.WithContext(ctx).
		Where("from_task_id IN ? OR to_task_id IN ?", taskIDs, taskIDs).
		Order("created_at ASC").Find(&edges).Error; err != nil {
		m.logger().Warn("batch load dependencies failed", "err", err)
		return out
	}
	for _, e := range edges {
		fromOut, fromIn := out[e.FromTaskID]
		toOut, toIn := out[e.ToTaskID]
		if e.Kind == "blocks" {
			if fromIn {
				fromOut.blockedBy = append(fromOut.blockedBy, e.ToTaskID)
				out[e.FromTaskID] = fromOut
			}
			if toIn {
				toOut.blocks = append(toOut.blocks, e.FromTaskID)
				out[e.ToTaskID] = toOut
			}
			continue
		}
		if fromIn {
			fromOut.related = append(fromOut.related, e.ToTaskID)
			out[e.FromTaskID] = fromOut
		}
		if toIn {
			toOut.related = append(toOut.related, e.FromTaskID)
			out[e.ToTaskID] = toOut
		}
	}
	return out
}

// depView 单任务便捷入口（批量版退化调用）。
func (m *Module) depView(ctx context.Context, taskID string) depViews {
	return m.depViewsForTasks(ctx, []string{taskID})[taskID]
}
