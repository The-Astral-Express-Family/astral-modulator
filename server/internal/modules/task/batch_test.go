// batch_test.go：批量管理三端点的服务层测试（协议 2.1 task_batch）。
// 重点断言：全有或全无（失败零落库）、树形镜像响应、批内互移环检测、
// revision/存在性整批失败、事件与 audit 的批量形状。
package task

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

func mustTree(t *testing.T, out taskTreesOut, err error) taskTreesOut {
	t.Helper()
	if err != nil {
		t.Fatalf("CreateTaskTrees: %v", err)
	}
	return out
}

// treeCall 直呼服务核心并要求成功。
func treeCall(t *testing.T, f *fixture, secret, wsID string, parent *string, in taskTreesIn) taskTreesOut {
	t.Helper()
	out, err := f.m.CreateTaskTrees(context.Background(), principal(t, f, secret), wsID, parent, in)
	if err != nil {
		t.Fatalf("CreateTaskTrees: %v", err)
	}
	return out
}

func treeTitles(nodes []taskTreeNodeDTO) []string {
	titles := make([]string, 0, len(nodes))
	for _, n := range nodes {
		titles = append(titles, n.Task.Title)
	}
	return titles
}

// TestCreateTaskTreesWorkspace：两棵根树（一棵带孙子层）一次建齐；
// 响应镜像请求嵌套结构，tags/children_count 恒填充，事件与 audit 落批。
func TestCreateTaskTreesWorkspace(t *testing.T) {
	f := setup(t)
	f.db.Create(&model.Tag{ID: "tag_1", WorkspaceID: f.wsID, Name: "Backend", NormalizedName: "backend"})

	in := taskTreesIn{Trees: []taskTreeNodeIn{
		{Title: "Epic A", Priority: "high", Tags: []string{"Backend"}, Children: []taskTreeNodeIn{
			{Title: "Story A1", Children: []taskTreeNodeIn{{Title: "Sub A1a"}}},
			{Title: "Story A2"},
		}},
		{Title: "Epic B"},
	}}
	out := treeCall(t, f, f.credA, f.wsID, nil, in)

	if got := treeTitles(out.Items); len(got) != 2 || got[0] != "Epic A" || got[1] != "Epic B" {
		t.Fatalf("roots = %v", got)
	}
	epic := out.Items[0]
	if len(epic.Children) != 2 || epic.Children[0].Task.Title != "Story A1" {
		t.Fatalf("epic children = %v", treeTitles(epic.Children))
	}
	if len(epic.Children[0].Children) != 1 || epic.Children[0].Children[0].Task.Title != "Sub A1a" {
		t.Fatalf("grandchild missing: %v", epic.Children[0].Children)
	}
	// 链接正确性：孙子 parent = Story A1，儿子 parent = Epic A。
	sub := epic.Children[0].Children[0].Task
	if sub.ParentID == nil || *sub.ParentID != epic.Children[0].Task.ID {
		t.Fatalf("grandchild parent_id = %v", sub.ParentID)
	}
	if epic.Task.ChildrenCount != 2 || epic.Children[0].Task.ChildrenCount != 1 {
		t.Fatalf("children_count not filled: %d/%d", epic.Task.ChildrenCount, epic.Children[0].Task.ChildrenCount)
	}
	if len(epic.Task.Tags) != 1 || epic.Task.Tags[0].Name != "Backend" {
		t.Fatalf("tags not filled: %v", epic.Task.Tags)
	}
	if epic.Task.Status != "open" || epic.Task.Priority != "high" || epic.Task.Revision != 1 {
		t.Fatalf("bad task fields: %+v", epic.Task)
	}
	// 根层 parent 必须为空。
	if out.Items[1].Task.ParentID != nil {
		t.Fatalf("root parent_id = %v", out.Items[1].Task.ParentID)
	}
	assertTaskCount(t, f, 5) // A + A1 + A1a + A2 + B
	assertOutbox(t, f, outbox.TypeTaskCreated, 5)
	assertAudit(t, f, "task.batch_create", 1)
}

// TestCreateTaskTreesUnderTask：task 容器版，整棵子树挂到容器任务下。
func TestCreateTaskTreesUnderTask(t *testing.T) {
	f := setup(t)
	parent := createTask(t, f, "tsk_p1", "parent")

	in := taskTreesIn{Trees: []taskTreeNodeIn{
		{Title: "Child 1", Children: []taskTreeNodeIn{{Title: "Grandchild"}}},
		{Title: "Child 2"},
	}}
	out := treeCall(t, f, f.credA, f.wsID, &parent.ID, in)
	for _, root := range out.Items {
		if root.Task.ParentID == nil || *root.Task.ParentID != parent.ID {
			t.Fatalf("root parent = %v, want container task", root.Task.ParentID)
		}
	}
	if out.Items[0].Children[0].Task.ParentID == nil || *out.Items[0].Children[0].Task.ParentID != out.Items[0].Task.ID {
		t.Fatalf("grandchild parent mismatch")
	}
}

// TestCreateTaskTreesAllOrNothing：任一节点非法 → 整批失败零落库；
// 未知 tag 名 → 404 整批不创建。
func TestCreateTaskTreesAllOrNothing(t *testing.T) {
	f := setup(t)
	f.db.Create(&model.Tag{ID: "tag_1", WorkspaceID: f.wsID, Name: "Backend", NormalizedName: "backend"})
	p := principal(t, f, f.credA)

	cases := []struct {
		name string
		in   taskTreesIn
		want int // 期望 HTTP 状态
	}{
		{"bad title in second tree", taskTreesIn{Trees: []taskTreeNodeIn{
			{Title: "ok"}, {Title: "  "},
		}}, 400},
		{"bad priority deep", taskTreesIn{Trees: []taskTreeNodeIn{
			{Title: "ok", Children: []taskTreeNodeIn{{Title: "n", Priority: "nope"}}},
		}}, 400},
		{"unknown tag", taskTreesIn{Trees: []taskTreeNodeIn{
			{Title: "ok", Tags: []string{"Backend"}}, {Title: "ok2", Tags: []string{"ghost"}},
		}}, 404},
		{"too many nodes", func() taskTreesIn {
			nodes := []taskTreeNodeIn{}
			for i := 0; i < 201; i++ {
				nodes = append(nodes, taskTreeNodeIn{Title: "n"})
			}
			return taskTreesIn{Trees: nodes}
		}(), 400},
		{"too deep", func() taskTreesIn {
			n := taskTreeNodeIn{Title: "leaf"}
			for i := 0; i < 8; i++ {
				n = taskTreeNodeIn{Title: "l", Children: []taskTreeNodeIn{n}}
			}
			return taskTreesIn{Trees: []taskTreeNodeIn{n}}
		}(), 400},
		{"empty trees", taskTreesIn{Trees: nil}, 400},
	}
	for _, tc := range cases {
		_, err := f.m.CreateTaskTrees(context.Background(), p, f.wsID, nil, tc.in)
		apiErr, ok := err.(*httpx.APIError)
		if !ok || apiErr.Status != tc.want {
			t.Fatalf("%s: got %v, want HTTP %d", tc.name, err, tc.want)
		}
	}
	assertTaskCount(t, f, 0)
	assertOutbox(t, f, outbox.TypeTaskCreated, 0)

	// 合法基线：深度恰好 8 层应该成功。
	n := taskTreeNodeIn{Title: "leaf"}
	for i := 0; i < 7; i++ {
		n = taskTreeNodeIn{Title: "l", Children: []taskTreeNodeIn{n}}
	}
	if _, err := f.m.CreateTaskTrees(context.Background(), p, f.wsID, nil, taskTreesIn{Trees: []taskTreeNodeIn{n}}); err != nil {
		t.Fatalf("depth-8 tree should pass: %v", err)
	}
	assertTaskCount(t, f, 8)
}

// TestMoveTasks：根↔子互移、null 回根、revision bump、事件按项发。
func TestMoveTasks(t *testing.T) {
	f := setup(t)
	a := createTask(t, f, "tsk_a", "A") // 根
	b := createTask(t, f, "tsk_b", "B") // 根
	b.ParentID = &a.ID
	f.db.Save(&b)
	c := createTask(t, f, "tsk_c", "C") // B 的子
	c.ParentID = &b.ID
	f.db.Save(&c)
	p := principal(t, f, f.credA)

	// B（携子树 C）移到根层；A 移到 B 下。
	out, err := f.m.MoveTasks(context.Background(), p, f.wsID, taskMoveIn{Items: []taskMoveItemIn{
		{TaskID: "tsk_b", ParentID: nil, ExpectedRevision: b.Revision},
		{TaskID: "tsk_a", ParentID: &b.ID, ExpectedRevision: a.Revision},
	}})
	if err != nil {
		t.Fatalf("MoveTasks: %v", err)
	}
	if len(out.Items) != 2 || out.Items[0].ID != "tsk_b" || out.Items[1].ID != "tsk_a" {
		t.Fatalf("items order = %v", out.Items)
	}
	if out.Items[0].ParentID != nil {
		t.Fatalf("B should be root, got %v", out.Items[0].ParentID)
	}
	if out.Items[1].ParentID == nil || *out.Items[1].ParentID != "tsk_b" {
		t.Fatalf("A should be under B, got %v", out.Items[1].ParentID)
	}
	if out.Items[0].Revision != b.Revision+1 || out.Items[1].Revision != a.Revision+1 {
		t.Fatalf("revision not bumped")
	}
	var cRow model.Task
	f.db.First(&cRow, "id = ?", "tsk_c")
	if cRow.ParentID == nil || *cRow.ParentID != "tsk_b" {
		t.Fatalf("grandchild C must keep parent B, got %v", cRow.ParentID)
	}
	assertOutbox(t, f, outbox.TypeTaskUpdated, 2)
	assertAudit(t, f, "task.batch_move", 1)
}

// TestMoveTasksFailures：环（含批内互移成环）、revision 冲突、不存在、
// 重复 task_id、超上限——全部整批失败零落库。
func TestMoveTasksFailures(t *testing.T) {
	f := setup(t)
	a := createTask(t, f, "tsk_a", "A")
	b := createTask(t, f, "tsk_b", "B")
	b.ParentID = &a.ID
	f.db.Save(&b)
	p := principal(t, f, f.credA)
	underB := b.ID

	cases := []struct {
		name string
		in   taskMoveIn
		want int
	}{
		{"move under own descendant", taskMoveIn{Items: []taskMoveItemIn{
			{TaskID: "tsk_a", ParentID: &underB, ExpectedRevision: a.Revision},
		}}, 400},
		{"batch mutual cycle", taskMoveIn{Items: []taskMoveItemIn{
			{TaskID: "tsk_a", ParentID: &b.ID, ExpectedRevision: a.Revision},
			{TaskID: "tsk_b", ParentID: &a.ID, ExpectedRevision: b.Revision},
		}}, 400},
		{"revision conflict", taskMoveIn{Items: []taskMoveItemIn{
			{TaskID: "tsk_a", ParentID: nil, ExpectedRevision: a.Revision + 5},
		}}, 409},
		{"missing task", taskMoveIn{Items: []taskMoveItemIn{
			{TaskID: "tsk_ghost", ParentID: nil, ExpectedRevision: 1},
		}}, 404},
		{"duplicate task_id", taskMoveIn{Items: []taskMoveItemIn{
			{TaskID: "tsk_a", ParentID: nil, ExpectedRevision: a.Revision},
			{TaskID: "tsk_a", ParentID: &b.ID, ExpectedRevision: a.Revision},
		}}, 400},
		{"over limit", func() taskMoveIn {
			items := []taskMoveItemIn{}
			for i := 0; i < 201; i++ {
				items = append(items, taskMoveItemIn{TaskID: "tsk_a", ExpectedRevision: a.Revision})
			}
			return taskMoveIn{Items: items}
		}(), 400},
	}
	for _, tc := range cases {
		_, err := f.m.MoveTasks(context.Background(), p, f.wsID, tc.in)
		apiErr, ok := err.(*httpx.APIError)
		if !ok || apiErr.Status != tc.want {
			t.Fatalf("%s: got %v, want HTTP %d", tc.name, err, tc.want)
		}
	}
	// 零落库：parent/revision 均未变。
	var aRow model.Task
	f.db.First(&aRow, "id = ?", "tsk_a")
	if aRow.ParentID != nil || aRow.Revision != a.Revision {
		t.Fatalf("all-or-nothing violated: parent=%v rev=%d", aRow.ParentID, aRow.Revision)
	}
	var bRow model.Task
	f.db.First(&bRow, "id = ?", "tsk_b")
	if bRow.ParentID == nil || *bRow.ParentID != a.ID || bRow.Revision != b.Revision {
		t.Fatalf("all-or-nothing violated on B: parent=%v rev=%d", bRow.ParentID, bRow.Revision)
	}
	assertOutbox(t, f, outbox.TypeTaskUpdated, 0)

	// revision 冲突 details 带 task_id + current_revision。
	_, err := f.m.MoveTasks(context.Background(), p, f.wsID, taskMoveIn{Items: []taskMoveItemIn{
		{TaskID: "tsk_a", ParentID: nil, ExpectedRevision: 999},
	}})
	apiErr := err.(*httpx.APIError)
	if apiErr.Details["task_id"] != "tsk_a" || apiErr.Details["current_revision"] != a.Revision {
		t.Fatalf("conflict details = %v", apiErr.Details)
	}
}

// TestBatchUpdateTasks：同值批量改状态/优先级/清指派；空 set 400；全有或全无。
func TestBatchUpdateTasks(t *testing.T) {
	f := setup(t)
	x := createTask(t, f, "tsk_x", "X")
	y := createTask(t, f, "tsk_y", "Y")
	p := principal(t, f, f.credA)

	in := taskBatchUpdateIn{Items: []taskBatchUpdateItemIn{
		{TaskID: "tsk_x", ExpectedRevision: x.Revision},
		{TaskID: "tsk_y", ExpectedRevision: y.Revision},
	}}
	in.Set.Status = strptr("done")
	in.Set.Priority = strptr("low")
	in.Set.AssigneeActorID = idPtr("agt_a1")
	out2, err := f.m.BatchUpdateTasks(context.Background(), p, f.wsID, in)
	if err != nil {
		t.Fatalf("BatchUpdateTasks: %v", err)
	}
	for _, dto := range out2.Items {
		if dto.Status != "done" || dto.Priority != "low" || dto.AssigneeActorID == nil || *dto.AssigneeActorID != "agt_a1" {
			t.Fatalf("batch update not applied: %+v", dto)
		}
	}
	assertOutbox(t, f, outbox.TypeTaskUpdated, 2)
	assertAudit(t, f, "task.batch_update", 1)

	// 清空指派：assignee_actor_id = null（三态指针内层）。
	in3 := taskBatchUpdateIn{Items: []taskBatchUpdateItemIn{{TaskID: "tsk_x", ExpectedRevision: x.Revision + 1}}}
	in3.Set.AssigneeActorID = nilPtr()
	if _, err := f.m.BatchUpdateTasks(context.Background(), p, f.wsID, in3); err != nil {
		t.Fatalf("clear assignee: %v", err)
	}
	var xRow model.Task
	f.db.First(&xRow, "id = ?", "tsk_x")
	if xRow.AssigneeActorID != nil {
		t.Fatalf("assignee not cleared: %v", xRow.AssigneeActorID)
	}

	// 失败路径：空 set / 非法 status / 不存在 / revision 冲突 → 整批失败。
	badCases := []struct {
		name string
		mk   func() taskBatchUpdateIn
		want int
	}{
		{"empty set", func() taskBatchUpdateIn {
			return taskBatchUpdateIn{Items: []taskBatchUpdateItemIn{{TaskID: "tsk_x", ExpectedRevision: x.Revision + 2}}}
		}, 400},
		{"bad status", func() taskBatchUpdateIn {
			in := taskBatchUpdateIn{Items: []taskBatchUpdateItemIn{{TaskID: "tsk_x", ExpectedRevision: x.Revision + 2}}}
			in.Set.Status = strptr("nope")
			return in
		}, 400},
		{"missing task", func() taskBatchUpdateIn {
			in := taskBatchUpdateIn{Items: []taskBatchUpdateItemIn{{TaskID: "tsk_ghost", ExpectedRevision: 1}}}
			in.Set.Status = strptr("done")
			return in
		}, 404},
	}
	for _, tc := range badCases {
		_, err := f.m.BatchUpdateTasks(context.Background(), p, f.wsID, tc.mk())
		apiErr, ok := err.(*httpx.APIError)
		if !ok || apiErr.Status != tc.want {
			t.Fatalf("%s: got %v, want HTTP %d", tc.name, err, tc.want)
		}
	}
}

func strptr(s string) *string { return &s }

// idPtr 三态字段的非空指派（**string 内层指向 id）。
func idPtr(s string) **string {
	v := &s
	return &v
}

func nilPtr() **string {
	inner := (*string)(nil)
	return &inner
}

// ---- 断言小件 ----

func assertTaskCount(t *testing.T, f *fixture, want int64) {
	t.Helper()
	var cnt int64
	f.db.Model(&model.Task{}).Where("workspace_id = ?", f.wsID).Count(&cnt)
	if cnt != want {
		t.Fatalf("task count = %d, want %d", cnt, want)
	}
}

func assertOutbox(t *testing.T, f *fixture, typ string, want int64) {
	t.Helper()
	var cnt int64
	f.db.Model(&model.OutboxEvent{}).Where("type = ?", typ).Count(&cnt)
	if cnt != want {
		t.Fatalf("outbox %s count = %d, want %d", typ, cnt, want)
	}
}

func assertAudit(t *testing.T, f *fixture, action string, want int64) {
	t.Helper()
	var rows []model.AuditEntry
	if err := f.db.Where("action = ?", action).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if int64(len(rows)) != want {
		t.Fatalf("audit %s count = %d, want %d", action, len(rows), want)
	}
	if want == 1 {
		var details map[string]any
		if err := json.Unmarshal(rows[0].Details, &details); err != nil {
			t.Fatalf("audit details unmarshal: %v", err)
		}
		if details["count"] == nil {
			t.Fatalf("audit details missing count summary: %v", details)
		}
	}
}
