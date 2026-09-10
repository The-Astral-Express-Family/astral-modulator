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
	//	  结构化+内容组合可用。
	code, errBody = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search", nil)
	if code != 400 {
		t.Fatalf("search zero-condition guard: %d %v", code, errBody)
	}
	code, page = doAuthed(t, ts, cookie, "GET", api+"/workspaces/"+wsID+"/task-search?tag=backend", nil)
	if code != 200 || len(items(t, page)) != 1 {
		t.Fatalf("tag-only search: %d %v", code, page)
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
