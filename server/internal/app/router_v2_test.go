// v2 容器化任务树的 HTTP 集成契约（TODO.md D15）：
// 根层/子层同构集合、tag/children_count 批量填充、task-search 零条件守卫、
// 旧端点（/workspaces/{id}/tasks、/tasks/search）移除后 404。
package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doAuthed 发送携带 Cookie 会话的 JSON 请求（human owner 全 scope）。
func doAuthed(t *testing.T, ts *httptest.Server, cookie, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "astral_session", Value: cookie})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// loginHuman 注册 + 登录，返回会话 Cookie（新测试服务器）。
func loginHuman(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	base := ts.URL + "/api/v1"
	code, reg := do(t, "POST", base+"/auth/register", "", map[string]any{
		"email": email, "password": "hunter2safe", "display_name": "Hime",
	})
	if code != 201 {
		t.Fatalf("register: %d %v", code, reg)
	}
	reqBody, _ := json.Marshal(map[string]any{"email": email, "password": "hunter2safe"})
	resp, err := http.Post(base+"/auth/login", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "astral_session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie set")
	return ""
}

func items(t *testing.T, body map[string]any) []any {
	t.Helper()
	arr, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("missing items array: %v", body)
	}
	return arr
}

func TestTaskTreeContainersV2(t *testing.T) {
	ts := newTestServer(t)
	cookie := loginHuman(t, ts, "tree@example.com")
	api := "/api/v1"

	code, ws := doAuthed(t, ts, cookie, "POST", api+"/workspaces", map[string]any{"name": "tree-demo"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)

	// 建一个真实 tag（两步确认），供 tags 批量填充与 tag 过滤用。
	code, prop := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tag-proposals",
		map[string]any{"action": "create", "name": "backend"})
	if code != 201 || prop["confirm_code"] == nil {
		t.Fatalf("propose tag: %d %v", code, prop)
	}
	code, tagRow := doAuthed(t, ts, cookie, "POST",
		api+"/tag-proposals/"+prop["proposal_id"].(string)+"/confirm",
		map[string]any{"confirm_code": prop["confirm_code"].(string), "name": "backend"})
	if code != 200 {
		t.Fatalf("confirm tag: %d %v", code, tagRow)
	}

	// 1. 根层创建：携带既有 tag 名 → 201，tags/children_count 恒填充。
	code, root := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/children",
		map[string]any{"title": "Epic: v2 migration", "tags": []string{"Backend"}})
	if code != 201 {
		t.Fatalf("create root: %d %v", code, root)
	}
	rootID := root["id"].(string)
	if root["parent_id"] != nil {
		t.Fatalf("root parent_id must be null: %v", root)
	}
	rootTags, ok := root["tags"].([]any)
	if !ok || len(rootTags) != 1 {
		t.Fatalf("root tags not filled: %v", root)
	}
	if root["children_count"] != float64(0) {
		t.Fatalf("root children_count: %v", root)
	}

	// 2. 未知 tag 名 → 404，整体不创建。
	code, errBody := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/children",
		map[string]any{"title": "ghost", "tags": []string{"nope"}})
	if code != 404 || errCode(t, errBody) != "NOT_FOUND" {
		t.Fatalf("unknown tag: %d %v", code, errBody)
	}

	// 3. 子层创建：投递进 task 容器 → parent 指向容器。
	code, child := doAuthed(t, ts, cookie, "POST", api+"/tasks/"+rootID+"/children",
		map[string]any{"title": "Write DDL"})
	if code != 201 {
		t.Fatalf("create child: %d %v", code, child)
	}
	childID := child["id"].(string)
	if child["parent_id"] != rootID {
		t.Fatalf("child parent_id = %v, want %s", child["parent_id"], rootID)
	}
	code, grandchild := doAuthed(t, ts, cookie, "POST", api+"/tasks/"+childID+"/children",
		map[string]any{"title": "Lint pass"})
	if code != 201 {
		t.Fatalf("create grandchild: %d %v", code, grandchild)
	}

	// 4. 根层集合：只含根任务，children_count=1（孙辈不计入）。
	code, page := doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children", nil)
	if code != 200 {
		t.Fatalf("list roots: %d %v", code, page)
	}
	rootItems := items(t, page)
	if len(rootItems) != 1 {
		t.Fatalf("root collection must only contain roots, got %d", len(rootItems))
	}
	if rootItems[0].(map[string]any)["children_count"] != float64(1) {
		t.Fatalf("root children_count: %v", rootItems[0])
	}

	// 5. 子层集合：直接子层（不含孙辈），同构参数（status 过滤）。
	code, page = doAuthed(t, ts, cookie, "GET", api+"/tasks/"+rootID+"/children", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("task children: %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/tasks/"+rootID+"/children?status=done", nil)
	if code != 200 || len(items(t, page)) != 0 {
		t.Fatalf("children status filter: %d %v", code, page)
	}

	// 6. task-search：零条件 400；纯 tag 条件 200（D15 修订的关键场景）；
	//	  结构化+内容组合可用；依赖过滤（2.2）单带也合法（2.6.1 放宽）。
	code, errBody = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search", nil)
	if code != 400 {
		t.Fatalf("search zero-condition guard: %d %v", code, errBody)
	}
	code, errBody = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?blocked=maybe", nil)
	if code != 400 {
		t.Fatalf("blocked 非布尔值不计入条件: %d %v", code, errBody)
	}
	code, errBody = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?blocked=false", nil)
	if code != 400 {
		t.Fatalf("blocked=false 不表达过滤,单独使用仍 400: %d %v", code, errBody)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?tag=backend&blocked=maybe", nil)
	if code != 200 {
		t.Fatalf("组合过滤下非法 blocked 被忽略: %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?tag=backend", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("tag-only search: %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?blocked=true", nil)
	if code != 200 || len(items(t, page)) != 0 {
		t.Fatalf("blocked-only search (no edges yet): %d %v", code, page)
	}
	// 建一条依赖边（child 依赖 root）后，blocked/blocked_by 单带可查且过滤生效。
	code, dep := doAuthed(t, ts, cookie, "PUT",
		api+"/tasks/"+childID+"/dependencies/"+rootID, map[string]any{"kind": "blocks"})
	if code != 201 && code != 200 {
		t.Fatalf("add dependency: %d %v", code, dep)
	}
	code, page = doAuthed(t, ts, cookie, "GET",
		api+"/workspaces/"+wsID+"/task-search?blocked_by="+rootID, nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("blocked_by-only search: %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?blocked=true", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("blocked-only search (edge present): %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?regex=DDL&fuzzy=migration", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("combined search: %d %v", code, page)
	}
	hit := items(t, page)[0].(map[string]any)
	if hit["tags"] == nil || hit["children_count"] == nil {
		t.Fatalf("search rows must carry tags/children_count: %v", hit)
	}

	// 7. v1 旧端点移除后 404（同路径不得复用新语义）。
	for _, legacy := range []string{
		api + "/workspaces/" + wsID + "/tasks",
		api + "/workspaces/" + wsID + "/tasks/search?regex=x",
	} {
		code, errBody = doAuthed(t, ts, cookie, "GET", legacy, nil)
		if code != 404 {
			t.Fatalf("legacy endpoint %s must be gone: %d %v", legacy, code, errBody)
		}
	}
}

// doAuthedHeader 同 doAuthed，可附带额外 header（Idempotency-Key 重放测试用）。
func doAuthedHeader(t *testing.T, ts *httptest.Server, cookie, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "astral_session", Value: cookie})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestTaskBatchV2：批量管理三端点的 HTTP 集成（协议 2.1 task_batch）——
// 树创建镜像形状、批量移动、批量同值更新、Idempotency-Key 整批重放不双建。
func TestTaskBatchV2(t *testing.T) {
	ts := newTestServer(t)
	cookie := loginHuman(t, ts, "batch@example.com")
	api := "/api/v1"
	code, ws := doAuthed(t, ts, cookie, "POST", api+"/workspaces", map[string]any{"name": "batch-demo"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)

	// 1. 树创建：一棵带子的树 + 一棵单节点树 → 201，镜像嵌套 + tags/children_count。
	body := map[string]any{"trees": []any{
		map[string]any{"title": "Epic", "children": []any{map[string]any{"title": "Story"}}},
		map[string]any{"title": "Solo"},
	}}
	code, out := doAuthedHeader(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/task-trees", body,
		map[string]string{"Idempotency-Key": "itest-batch-tree-1"})
	if code != 201 {
		t.Fatalf("task-trees: %d %v", code, out)
	}
	created := items(t, out)
	if len(created) != 2 {
		t.Fatalf("tree roots = %d", len(created))
	}
	epic := created[0].(map[string]any)["task"].(map[string]any)
	epicID := epic["id"].(string)
	kids := created[0].(map[string]any)["children"].([]any)
	if len(kids) != 1 || kids[0].(map[string]any)["task"].(map[string]any)["parent_id"] != epicID {
		t.Fatalf("tree nesting broken: %v", created[0])
	}
	soloID := created[1].(map[string]any)["task"].(map[string]any)["id"].(string)

	// 2. 同 Idempotency-Key 重放 → 同响应，且根层不重复建树。
	code2, out2 := doAuthedHeader(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/task-trees", body,
		map[string]string{"Idempotency-Key": "itest-batch-tree-1"})
	if code2 != 201 {
		t.Fatalf("idempotent replay: %d %v", code2, out2)
	}
	replayed := items(t, out2)
	if replayed[0].(map[string]any)["task"].(map[string]any)["id"] != epicID {
		t.Fatalf("replay must return first response, got new ids")
	}
	code, page := doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children", nil)
	if code != 200 || len(items(t, page)) != 2 {
		t.Fatalf("replay duplicated trees: %d %v", code, page)
	}

	// 3. 批量移动：Solo 移到 Epic 下；Story 移回根层（parent_id=null）。
	storyID := kids[0].(map[string]any)["task"].(map[string]any)["id"].(string)
	code, mv := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/move",
		map[string]any{"items": []any{
			map[string]any{"task_id": soloID, "parent_id": epicID, "expected_revision": 1},
			map[string]any{"task_id": storyID, "parent_id": nil, "expected_revision": 1},
		}})
	if code != 200 {
		t.Fatalf("move: %d %v", code, mv)
	}
	moved := items(t, mv)
	if moved[0].(map[string]any)["parent_id"] != epicID || moved[1].(map[string]any)["parent_id"] != nil {
		t.Fatalf("move result: %v", moved)
	}
	if moved[0].(map[string]any)["revision"] != float64(2) {
		t.Fatalf("move must bump revision: %v", moved[0])
	}

	// 4. 批量同值更新：全 done。set 为空 → 400。
	code, up := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/batch-update",
		map[string]any{
			"items": []any{
				map[string]any{"task_id": epicID, "expected_revision": 1}, // Epic 未被移动，仍在 rev1
				map[string]any{"task_id": soloID, "expected_revision": 2},
			},
			"set": map[string]any{"status": "done"},
		})
	if code != 200 {
		t.Fatalf("batch-update: %d %v", code, up)
	}
	for _, it := range items(t, up) {
		if it.(map[string]any)["status"] != "done" {
			t.Fatalf("status not applied: %v", it)
		}
	}
	code, errBody := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/batch-update",
		map[string]any{"items": []any{map[string]any{"task_id": epicID, "expected_revision": 3}}, "set": map[string]any{}})
	if code != 400 || errCode(t, errBody) != "VALIDATION_FAILED" {
		t.Fatalf("empty set: %d %v", code, errBody)
	}

	// 5. 环：Epic 移到自己的子孙 Solo 下 → 400 整批不生效。
	code, errBody = doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/move",
		map[string]any{"items": []any{
			map[string]any{"task_id": epicID, "parent_id": soloID, "expected_revision": 2},
		}})
	if code != 400 || errCode(t, errBody) != "VALIDATION_FAILED" {
		t.Fatalf("cycle: %d %v", code, errBody)
	}

	// 6. revision 冲突 → 409，details 带 task_id。
	code, errBody = doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/move",
		map[string]any{"items": []any{
			map[string]any{"task_id": epicID, "parent_id": nil, "expected_revision": 99},
		}})
	if code != 409 || errCode(t, errBody) != "REVISION_CONFLICT" {
		t.Fatalf("revision conflict: %d %v", code, errBody)
	}
}

// TestTaskAssigneeNullSemanticsV2（协议 2.6.1）：assignee_actor_id 显式
// null = 清空、缺席 = 不改（PATCH 与 batch-update 同口径）。**string 在
// encoding/json 下 null 与缺席不可区分，HTTP 层必须 RawMessage 分辨。
func TestTaskAssigneeNullSemanticsV2(t *testing.T) {
	ts := newTestServer(t)
	cookie := loginHuman(t, ts, "assignee-null@example.com")
	api := "/api/v1"
	code, ws := doAuthed(t, ts, cookie, "POST", api+"/workspaces", map[string]any{"name": "assignee-null"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)

	code, tree := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/task-trees",
		map[string]any{"trees": []any{map[string]any{"title": "A"}, map[string]any{"title": "B"}}})
	if code != 201 {
		t.Fatalf("task-trees: %d %v", code, tree)
	}
	created := items(t, tree)
	a := created[0].(map[string]any)["task"].(map[string]any)
	aID := a["id"].(string)
	bID := created[1].(map[string]any)["task"].(map[string]any)["id"].(string)

	// 认领 A → assignee 指派给自己（rev 2, in_progress）。
	code, claimed := doAuthed(t, ts, cookie, "POST", api+"/tasks/"+aID+"/claim",
		map[string]any{"expected_revision": 1})
	if code != 200 {
		t.Fatalf("claim: %d %v", code, claimed)
	}

	// PATCH 缺席 assignee 字段 = 不改：只改 title，assignee 保持自己。
	code, patched := doAuthed(t, ts, cookie, "PATCH", api+"/tasks/"+aID,
		map[string]any{"expected_revision": 2, "title": "renamed"})
	if code != 200 {
		t.Fatalf("patch absent: %d %v", code, patched)
	}
	if patched["assignee_actor_id"] == nil || patched["title"] != "renamed" {
		t.Fatalf("absent assignee must not change: %v", patched)
	}

	// PATCH 显式 null = 清空：assignee 归 nil，其余字段不动。
	code, patched = doAuthed(t, ts, cookie, "PATCH", api+"/tasks/"+aID,
		map[string]any{"expected_revision": 3, "assignee_actor_id": nil})
	if code != 200 {
		t.Fatalf("patch null: %d %v", code, patched)
	}
	if patched["assignee_actor_id"] != nil || patched["status"] != "in_progress" {
		t.Fatalf("null must clear assignee only: %v", patched)
	}

	// batch-update：HTTP 层显式 null 同样清空（service 层三态原样保留）。
	code, claimed = doAuthed(t, ts, cookie, "POST", api+"/tasks/"+bID+"/claim",
		map[string]any{"expected_revision": 1})
	if code != 200 {
		t.Fatalf("claim B: %d %v", code, claimed)
	}
	code, batched := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/batch-update",
		map[string]any{
			"items": []any{map[string]any{"task_id": bID, "expected_revision": 2}},
			"set":   map[string]any{"assignee_actor_id": nil},
		})
	if code != 200 {
		t.Fatalf("batch null: %d %v", code, batched)
	}
	if got := items(t, batched)[0].(map[string]any)["assignee_actor_id"]; got != nil {
		t.Fatalf("batch null must clear assignee: %v", got)
	}
}

// TestTaskPositionOrderingAndCursor（协议 2.4）：children 按 position 升序返回；
// 创建追加尾部；move 带 position 同调用完成换父与重排；游标为 position 键集
// （对客户端不透明，翻页续传即可）。
func TestTaskPositionOrderingAndCursor(t *testing.T) {
	ts := newTestServer(t)
	cookie := loginHuman(t, ts, "position@example.com")
	api := "/api/v1"

	code, ws := doAuthed(t, ts, cookie, "POST", api+"/workspaces", map[string]any{"name": "pos-demo"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)

	ids := make([]string, 0, 3)
	for _, title := range []string{"first", "second", "third"} {
		code, row := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/children",
			map[string]any{"title": title})
		if code != 201 {
			t.Fatalf("create %s: %d %v", title, code, row)
		}
		ids = append(ids, row["id"].(string))
		if row["position"] != float64(len(ids)-1) {
			t.Fatalf("create must append tail: %s position %v", title, row["position"])
		}
	}

	// 排序契约：position 升序 = 创建正序（first, second, third）。
	assertRootOrder := func(want []string) {
		t.Helper()
		code, page := doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children", nil)
		if code != 200 {
			t.Fatalf("list children: %d %v", code, page)
		}
		got := items(t, page)
		if len(got) != len(want) {
			t.Fatalf("root count = %d, want %d", len(got), len(want))
		}
		for i, id := range want {
			if got[i].(map[string]any)["id"] != id {
				t.Fatalf("root[%d] = %v, want %s", i, got[i], id)
			}
		}
	}
	assertRootOrder(ids)

	// 重排：third 移到 0 位 → [third, first, second]（其余 revision 不动）。
	code, mv := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/move",
		map[string]any{"items": []any{
			map[string]any{"task_id": ids[2], "parent_id": nil, "expected_revision": 1, "position": 0},
		}})
	if code != 200 {
		t.Fatalf("move with position: %d %v", code, mv)
	}
	if mv["items"].([]any)[0].(map[string]any)["position"] != float64(0) {
		t.Fatalf("move result position: %v", mv)
	}
	assertRootOrder([]string{ids[2], ids[0], ids[1]})

	// position 键集游标：limit=2 首页 + 续页恰好覆盖剩余（无重复无遗漏）。
	code, page := doAuthed(t, ts, cookie,
		"GET", api+"/workspaces/"+wsID+"/children?limit=2", nil)
	if code != 200 || len(items(t, page)) != 2 || page["next_cursor"] == nil {
		t.Fatalf("cursor page 1: %d %v", code, page)
	}
	firstTwo := items(t, page)
	code, page2 := doAuthed(t, ts, cookie,
		"GET", api+"/workspaces/"+wsID+"/children?limit=2&cursor="+page["next_cursor"].(string), nil)
	if code != 200 || len(items(t, page2)) != 1 {
		t.Fatalf("cursor page 2: %d %v", code, page2)
	}
	last := items(t, page2)[0].(map[string]any)["id"].(string)
	if firstTwo[0].(map[string]any)["id"] == last ||
		firstTwo[1].(map[string]any)["id"] == last {
		t.Fatalf("cursor page overlap: %v / %v", firstTwo, last)
	}

	// 换父 + 序位一次完成：third 挂到 first 下 0 位（first 无子）。
	code, mv = doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/tasks/move",
		map[string]any{"items": []any{
			map[string]any{"task_id": ids[2], "parent_id": ids[0], "expected_revision": 2, "position": 0},
		}})
	if code != 200 {
		t.Fatalf("cross-parent move: %d %v", code, mv)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/tasks/"+ids[0]+"/children", nil)
	if code != 200 || len(items(t, page)) != 1 || items(t, page)[0].(map[string]any)["id"] != ids[2] {
		t.Fatalf("children after cross-parent move: %d %v", code, page)
	}
	assertRootOrder([]string{ids[0], ids[1]})
}

// TestTaskDependenciesV2：依赖边三端点的 HTTP 集成（协议 2.2）——PUT 幂等、
// 视图字段恒填充、blocked/blocked_by 过滤、环 400、删除幂等 204。
func TestTaskDependenciesV2(t *testing.T) {
	ts := newTestServer(t)
	cookie := loginHuman(t, ts, "deps@example.com")
	api := "/api/v1"
	code, ws := doAuthed(t, ts, cookie, "POST", api+"/workspaces", map[string]any{"name": "deps-demo"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)
	mkTask := func(title string) string {
		code, out := doAuthed(t, ts, cookie, "POST", api+"/workspaces/"+wsID+"/children",
			map[string]any{"title": title})
		if code != 201 {
			t.Fatalf("create %s: %d %v", title, code, out)
		}
		return out["id"].(string)
	}
	a := mkTask("A") // 依赖 B
	b := mkTask("B")
	c := mkTask("C") // 无依赖

	// 1. PUT 建边：A 依赖 B → 201；重复 PUT → 200 幂等。
	depPath := func(from, to string) string {
		return api + "/tasks/" + from + "/dependencies/" + to
	}
	code, out := doAuthed(t, ts, cookie, "PUT", depPath(a, b), map[string]any{"kind": "blocks"})
	if code != 201 {
		t.Fatalf("put dep: %d %v", code, out)
	}
	code, out = doAuthed(t, ts, cookie, "PUT", depPath(a, b), map[string]any{})
	if code != 200 {
		t.Fatalf("idempotent put dep: %d %v", code, out)
	}

	// 2. Task 视图恒填充：A.blocked_by=[B]。
	code, out = doAuthed(t, ts, cookie, "GET", api+"/tasks/"+a, nil)
	if code != 200 {
		t.Fatalf("get A: %d %v", code, out)
	}
	if bb, ok := out["blocked_by"].([]any); !ok || len(bb) != 1 || bb[0] != b {
		t.Fatalf("A.blocked_by = %v", out["blocked_by"])
	}
	if blocks, ok := out["blocks"].([]any); !ok || len(blocks) != 0 {
		t.Fatalf("A.blocks = %v", out["blocks"])
	}

	// 3. 过滤：blocked=true 只回 A；blocked_by=B 只回 A。
	code, page := doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children?blocked=true", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("blocked filter: %d %v", code, page)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children?blocked_by="+b, nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("blocked_by filter: %d %v", code, page)
	}

	// 4. 环：B 依赖 A → 400。
	code, errBody := doAuthed(t, ts, cookie, "PUT", depPath(b, a), map[string]any{})
	if code != 400 || errCode(t, errBody) != "VALIDATION_FAILED" {
		t.Fatalf("cycle: %d %v", code, errBody)
	}

	// 5. 依赖边列表。
	code, out = doAuthed(t, ts, cookie, "GET", api+"/tasks/"+a+"/dependencies", nil)
	if code != 200 {
		t.Fatalf("list deps: %d %v", code, out)
	}
	if edges, ok := out["items"].([]any); !ok || len(edges) != 1 {
		t.Fatalf("dep list = %v", out)
	}

	// 6. DELETE 幂等 204；再查 blocked 过滤为空。
	code, _ = doAuthed(t, ts, cookie, "DELETE", depPath(a, b)+"?kind=blocks", nil)
	if code != 204 {
		t.Fatalf("delete dep: %d", code)
	}
	code, _ = doAuthed(t, ts, cookie, "DELETE", depPath(a, b)+"?kind=blocks", nil)
	if code != 204 {
		t.Fatalf("idempotent delete dep: %d", code)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/children?blocked=true", nil)
	if code != 200 || len(items(t, page)) != 0 {
		t.Fatalf("blocked filter after delete: %d %v", code, page)
	}
	_ = c
}
