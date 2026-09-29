// dependencies_test.go：任务依赖边的服务层测试（协议 2.2 task_dependencies）。
// 重点断言：方向语义（from 依赖 to）、blocks 防环（含多分叉链）、relates
// 不参与环、幂等 PUT/DELETE 不 bump、两端 revision+1 + task.updated（两端）、
// audit 一条、视图字段（blocked_by/blocks/related）批量装填、blocked 过滤。
package task

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

func depsActor(t *testing.T, f *fixture) *auth.Principal {
	return principal(t, f, f.credA)
}

// add 便捷入口：要求成功且新建。
func addOK(t *testing.T, f *fixture, fromID, toID, kind string) {
	t.Helper()
	created, err := f.m.AddDependency(context.Background(), depsActor(t, f),
		taskByID(t, f, fromID), taskByID(t, f, toID), kind)
	if err != nil {
		t.Fatalf("AddDependency %s->%s(%s): %v", fromID, toID, kind, err)
	}
	if !created {
		t.Fatalf("AddDependency %s->%s(%s): expected new edge", fromID, toID, kind)
	}
}

// assertDepAudit 依赖边的 audit 断言（details 形状 {kind, peer}，无 count——
// 与批量端点的 count 摘要断言不同源）。
func assertDepAudit(t *testing.T, f *fixture, action string, want int) {
	t.Helper()
	var rows []model.AuditEntry
	if err := f.db.Where("action = ?", action).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != want {
		t.Fatalf("audit %s count = %d, want %d", action, len(rows), want)
	}
	if want == 1 {
		var details map[string]any
		if err := json.Unmarshal(rows[0].Details, &details); err != nil {
			t.Fatalf("audit details unmarshal: %v", err)
		}
		if details["peer"] == nil || details["kind"] == nil {
			t.Fatalf("audit details missing peer/kind: %v", details)
		}
	}
}

func taskByID(t *testing.T, f *fixture, id string) *model.Task {
	t.Helper()
	var row model.Task
	if err := f.db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}

func revOf(t *testing.T, f *fixture, id string) int64 {
	t.Helper()
	return taskByID(t, f, id).Revision
}

// TestDependencyAddAndView：A 依赖 B → A.blocked_by=[B]、B.blocks=[A]；
// 两端各 bump revision、各发一条 task.updated、audit 一条。
func TestDependencyAddAndView(t *testing.T) {
	f := setup(t)
	a := createTask(t, f, "tsk_a", "A")
	b := createTask(t, f, "tsk_b", "B")
	revA, revB := a.Revision, b.Revision

	addOK(t, f, "tsk_a", "tsk_b", "blocks")

	view := f.m.depView(context.Background(), "tsk_a")
	if len(view.blockedBy) != 1 || view.blockedBy[0] != "tsk_b" {
		t.Fatalf("A.blocked_by = %v", view.blockedBy)
	}
	if len(view.blocks) != 0 || len(view.related) != 0 {
		t.Fatalf("A.blocks/related = %v/%v", view.blocks, view.related)
	}
	viewB := f.m.depView(context.Background(), "tsk_b")
	if len(viewB.blocks) != 1 || viewB.blocks[0] != "tsk_a" {
		t.Fatalf("B.blocks = %v", viewB.blocks)
	}
	if len(viewB.blockedBy) != 0 {
		t.Fatalf("B.blocked_by = %v", viewB.blockedBy)
	}
	if got := revOf(t, f, "tsk_a"); got != revA+1 {
		t.Fatalf("A revision = %d, want %d", got, revA+1)
	}
	if got := revOf(t, f, "tsk_b"); got != revB+1 {
		t.Fatalf("B revision = %d, want %d", got, revB+1)
	}
	assertOutbox(t, f, outbox.TypeTaskUpdated, 2)
	assertDepAudit(t, f, "task.dependency.add", 1)
}

// TestDependencyIdempotentPut：重复 PUT 同边 → created=false 且不再 bump。
func TestDependencyIdempotentPut(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_a", "A")
	createTask(t, f, "tsk_b", "B")
	addOK(t, f, "tsk_a", "tsk_b", "blocks")
	revA := revOf(t, f, "tsk_a")

	created, err := f.m.AddDependency(context.Background(), depsActor(t, f),
		taskByID(t, f, "tsk_a"), taskByID(t, f, "tsk_b"), "blocks")
	if err != nil || created {
		t.Fatalf("idempotent put: created=%v err=%v", created, err)
	}
	if got := revOf(t, f, "tsk_a"); got != revA {
		t.Fatalf("idempotent put bumped revision: %d", got)
	}
}

// TestDependencyCycleRejected：直环与多分叉长链环都拒绝；relates 不参与环。
func TestDependencyCycleRejected(t *testing.T) {
	f := setup(t)
	for _, id := range []string{"tsk_a", "tsk_b", "tsk_c", "tsk_d"} {
		createTask(t, f, id, id)
	}

	addOK(t, f, "tsk_b", "tsk_a", "blocks") // B 依赖 A
	addOK(t, f, "tsk_c", "tsk_b", "blocks") // C 依赖 B
	// A 依赖 C：A=>C=>B=>A 成环（链深 2，验证多跳遍历）。
	if _, err := f.m.AddDependency(context.Background(), depsActor(t, f),
		taskByID(t, f, "tsk_a"), taskByID(t, f, "tsk_c"), "blocks"); err == nil {
		t.Fatal("cycle A->C->B->A must be rejected")
	}
	// 自依赖。
	if _, err := f.m.AddDependency(context.Background(), depsActor(t, f),
		taskByID(t, f, "tsk_a"), taskByID(t, f, "tsk_a"), "blocks"); err == nil {
		t.Fatal("self dependency must be rejected")
	}
	// relates 不参与环检测：A-relates-C 合法。
	addOK(t, f, "tsk_a", "tsk_c", "relates")
	// 无关分支：D 依赖 A 合法（不成环）。
	addOK(t, f, "tsk_d", "tsk_a", "blocks")
}

// TestDependencyRemove：删边幂等、两端 bump、audit 记 remove。
func TestDependencyRemove(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_a", "A")
	createTask(t, f, "tsk_b", "B")
	addOK(t, f, "tsk_a", "tsk_b", "blocks")

	p := depsActor(t, f)
	if err := f.m.RemoveDependency(context.Background(), p,
		taskByID(t, f, "tsk_a"), taskByID(t, f, "tsk_b"), "blocks"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	view := f.m.depView(context.Background(), "tsk_a")
	if len(view.blockedBy) != 0 {
		t.Fatalf("blocked_by after remove = %v", view.blockedBy)
	}
	// 幂等：再删不报错不再 bump。
	revA := revOf(t, f, "tsk_a")
	if err := f.m.RemoveDependency(context.Background(), p,
		taskByID(t, f, "tsk_a"), taskByID(t, f, "tsk_b"), "blocks"); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
	if got := revOf(t, f, "tsk_a"); got != revA {
		t.Fatalf("idempotent remove bumped revision: %d", got)
	}
	assertDepAudit(t, f, "task.dependency.remove", 1)
}

// TestBlockedFilter：blocked=true 只留存在未完成依赖的任务；依赖 done 后不再算阻塞。
func TestBlockedFilter(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_a", "A") // 依赖 B
	createTask(t, f, "tsk_b", "B") // blocker
	createTask(t, f, "tsk_c", "C") // 无依赖
	addOK(t, f, "tsk_a", "tsk_b", "blocks")

	// blocked=true：只有 A。
	rows := filterChildren(t, f, true, "")
	if len(rows) != 1 || rows[0].ID != "tsk_a" {
		t.Fatalf("blocked filter = %v", rows)
	}
	// blocked_by=B：只有 A。
	rows = filterChildren(t, f, false, "tsk_b")
	if len(rows) != 1 || rows[0].ID != "tsk_a" {
		t.Fatalf("blocked_by filter = %v", rows)
	}
	// B 完成：A 不再 blocked。
	b := taskByID(t, f, "tsk_b")
	b.Status = "done"
	f.db.Save(b)
	rows = filterChildren(t, f, true, "")
	if len(rows) != 0 {
		t.Fatalf("blocked filter after done = %v", rows)
	}
	// blocked_by 边仍在（过滤只看完成态的是 blocked）。
	rows = filterChildren(t, f, false, "tsk_b")
	if len(rows) != 1 || rows[0].ID != "tsk_a" {
		t.Fatalf("blocked_by filter after done = %v", rows)
	}
}

// filterChildren 直接走 listChildren 的过滤核心（taskFilters 的 EXISTS 子句
// 经 gorm 组装，这里以同参数查询验证）。
func filterChildren(t *testing.T, f *fixture, blocked bool, blockedBy string) []model.Task {
	t.Helper()
	query := f.db.Model(&model.Task{}).Where("workspace_id = ?", f.wsID).Where("parent_id IS NULL")
	filtered, apiErr := applyTaskFilters(query, taskFilters{Blocked: blocked, BlockedBy: blockedBy})
	if apiErr != nil {
		t.Fatalf("applyTaskFilters: %v", apiErr)
	}
	var rows []model.Task
	if err := filtered.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestDependencyEnrichInList：children 集合行内依赖视图恒填充（批量路径）。
func TestDependencyEnrichInList(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_a", "A")
	createTask(t, f, "tsk_b", "B")
	createTask(t, f, "tsk_c", "C")
	addOK(t, f, "tsk_a", "tsk_b", "blocks")
	addOK(t, f, "tsk_a", "tsk_c", "relates")

	var rows []model.Task
	if err := f.db.Where("workspace_id = ? AND parent_id IS NULL", f.wsID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	dtos := f.m.enrichTasks(context.Background(), rows)
	byID := map[string]taskDTO{}
	for _, dto := range dtos {
		byID[dto.ID] = dto
	}
	if got := byID["tsk_a"].BlockedBy; len(got) != 1 || got[0] != "tsk_b" {
		t.Fatalf("list row A.blocked_by = %v", got)
	}
	if got := byID["tsk_a"].Related; len(got) != 1 || got[0] != "tsk_c" {
		t.Fatalf("list row A.related = %v", got)
	}
	if got := byID["tsk_b"].Blocks; len(got) != 1 || got[0] != "tsk_a" {
		t.Fatalf("list row B.blocks = %v", got)
	}
	if got := byID["tsk_c"].Blocks; len(got) != 0 {
		t.Fatalf("list row C.blocks = %v", got)
	}
	if byID["tsk_c"].BlockedBy == nil || byID["tsk_c"].Blocks == nil || byID["tsk_c"].Related == nil {
		t.Fatal("dependency view fields must be filled (empty slices, not null)")
	}
}
