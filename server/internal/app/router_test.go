// Package app 的 HTTP 集成测试（sqlite 内存库）：
// 覆盖 CLI 联调关键契约——well-known、capabilities、401 鉴权边界、错误 envelope、
// 以及 roadmap spike 全链路：register → login → device approve → workspace →
// agent credential → create task → claim 竞争 → presence → message。
package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/idempotency"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/document"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/memory"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/message"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/presence"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/task"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/workspace"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, log)
	hub := event.NewHub()
	mods := &Modules{
		Idempotency: &idempotency.Middleware{DB: db, Log: log},
		Auth:        &auth.Module{Svc: svc, PublicURL: "https://astral.example.com"},
		Workspace:   &workspace.Module{DB: db, Auth: svc},
		Task:        &task.Module{DB: db, Auth: svc},
		Tag:         &tag.Module{DB: db, Auth: svc},
		Memory:      &memory.Module{},
		Document:    &document.Module{},
		Message:     &message.Module{DB: db, Auth: svc},
		Presence:    &presence.Module{DB: db, Auth: svc},
		Audit:       &audit.Module{},
		Events:      &event.SSEHandler{Hub: hub, DB: db, Auth: svc},
	}
	mods.Message.Tasks = mods.Task
	ts := httptest.NewServer(NewRouter(config.Config{ServerID: "srv_test", PublicURL: "https://astral.example.com"}, log, db, mods))
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

// errCode 从错误 envelope 提取 code。
func errCode(t *testing.T, body map[string]any) string {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	c, _ := e["code"].(string)
	return c
}

func TestWellKnownAndCapabilities(t *testing.T) {
	ts := newTestServer(t)

	code, wk := do(t, "GET", ts.URL+"/.well-known/astral", "", nil)
	if code != 200 || wk["server_id"] != "srv_test" || wk["api_base"] != "/api/v1" {
		t.Fatalf("well-known: %d %v", code, wk)
	}
	code, caps := do(t, "GET", ts.URL+"/api/v1/meta/capabilities", "", nil)
	if code != 200 || caps["protocol_version"] != float64(2) {
		t.Fatalf("capabilities: %d %v", code, caps)
	}
}

func TestProtectedWithoutTokenIs401(t *testing.T) {
	ts := newTestServer(t)

	code, body := do(t, "POST", ts.URL+"/api/v1/workspaces", "", map[string]any{"name": "x"})
	if code != 401 || errCode(t, body) != "AUTH_REQUIRED" {
		t.Fatalf("want 401 AUTH_REQUIRED, got %d %v", code, body)
	}
}

// TestSpikeE2E 是 roadmap Phase 1 验收的服务端等价物。
func TestSpikeE2E(t *testing.T) {
	ts := newTestServer(t)
	base := ts.URL + "/api/v1"

	// 1. bootstrap 注册。
	code, reg := do(t, "POST", base+"/auth/register", "", map[string]any{
		"email": "hime@example.com", "password": "hunter2safe", "display_name": "Hime",
	})
	if code != 201 {
		t.Fatalf("register: %d %v", code, reg)
	}

	// 2. CLI 发起 device flow。
	code, created := do(t, "POST", base+"/auth/device/authorizations", "", map[string]any{"client_type": "cli"})
	if code != 201 {
		t.Fatalf("device create: %d %v", code, created)
	}
	deviceCode := created["device_code"].(string)
	userCode := created["user_code"].(string)

	// pending → AUTHORIZATION_PENDING。
	code, pend := do(t, "POST", base+"/auth/device/authorizations/"+deviceCode+"/token", "", nil)
	if code != 400 || errCode(t, pend) != "AUTHORIZATION_PENDING" {
		t.Fatalf("pending exchange: %d %v", code, pend)
	}

	// 3. 浏览器侧：login（Cookie session）→ 查询 → 批准。
	var cookie string
	{
		reqBody, _ := json.Marshal(map[string]any{"email": "hime@example.com", "password": "hunter2safe"})
		resp, err := http.Post(base+"/auth/login", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("login: %d", resp.StatusCode)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "astral_session" {
				cookie = c.Value
			}
		}
		resp.Body.Close()
	}
	if cookie == "" {
		t.Fatal("no session cookie set")
	}
	{
		req, err := http.NewRequest("GET", base+"/auth/device/authorizations?user_code="+userCode, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: "astral_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var view map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&view)
		resp.Body.Close()
		if resp.StatusCode != 200 || view["status"] != "pending" {
			t.Fatalf("device view: %d %v", resp.StatusCode, view)
		}
		authID := view["id"].(string)
		req2, _ := http.NewRequest("POST", base+"/auth/device/authorizations/"+authID+"/approve", nil)
		req2.AddCookie(&http.Cookie{Name: "astral_session", Value: cookie})
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatal(err)
		}
		resp2.Body.Close()
		if resp2.StatusCode != 204 {
			t.Fatalf("approve: %d", resp2.StatusCode)
		}
	}

	// 4. CLI 兑换 token（含 /auth/me 自检）。
	code, cliPair := do(t, "POST", base+"/auth/device/authorizations/"+deviceCode+"/token", "", nil)
	if code != 200 || cliPair["access_token"] == nil {
		t.Fatalf("exchange: %d %v", code, cliPair)
	}
	humanAccess := cliPair["access_token"].(string)
	code, me := do(t, "GET", base+"/auth/me", humanAccess, nil)
	if code != 200 || me["actor"] == nil {
		t.Fatalf("me: %d %v", code, me)
	}

	// 5. 建 workspace（创建者自动 owner）。
	code, ws := do(t, "POST", base+"/workspaces", humanAccess, map[string]any{"name": "astral-modulator"})
	if code != 201 {
		t.Fatalf("create ws: %d %v", code, ws)
	}
	wsID := ws["id"].(string)

	// 6. 创建两个 agent 并签发 workspace 绑定的 credential。
	mkAgent := func(name string) string {
		code, agent := do(t, "POST", base+"/workspaces/"+wsID+"/agents", humanAccess,
			map[string]any{"display_name": name, "kind": "agent"})
		if code != 201 {
			t.Fatalf("create agent: %d %v", code, agent)
		}
		agentID := agent["id"].(string)
		code, cred := do(t, "POST", base+"/agents/"+agentID+"/credentials", humanAccess,
			map[string]any{"scopes": []string{"task:read", "task:write", "task:claim", "presence:write", "message:read", "message:send"}, "workspace_id": wsID})
		if code != 201 || cred["secret"] == nil {
			t.Fatalf("issue credential: %d %v", code, cred)
		}
		return cred["secret"].(string)
	}
	secretA := mkAgent("March 7th")
	secretB := mkAgent("Dan Heng")

	// 7. agent 建 task；两 credential 竞争 claim 只有一个成功。
	code, tk := do(t, "POST", base+"/workspaces/"+wsID+"/children", secretA,
		map[string]any{"title": "Implement spike"})
	if code != 201 {
		t.Fatalf("create task: %d %v", code, tk)
	}
	taskID := tk["id"].(string)

	code, claim1 := do(t, "POST", base+"/tasks/"+taskID+"/claim", secretA,
		map[string]any{"expected_revision": 1, "lease_seconds": 300})
	if code != 200 {
		t.Fatalf("claim1: %d %v", code, claim1)
	}
	code, claim2 := do(t, "POST", base+"/tasks/"+taskID+"/claim", secretB, nil)
	if code != 409 || errCode(t, claim2) != "TASK_ALREADY_CLAIMED" {
		t.Fatalf("claim2: %d %v", code, claim2)
	}

	// 8. presence + message（spike 链路收尾）。
	code, _ = do(t, "PUT", base+"/workspaces/"+wsID+"/presence/me", secretA,
		map[string]any{"state": "working", "current_task_id": taskID, "ttl_seconds": 90})
	if code != 200 {
		t.Fatalf("presence: %d", code)
	}
	code, msg := do(t, "POST", base+"/workspaces/"+wsID+"/messages", secretA,
		map[string]any{"body": "claimed the spike task", "target": map[string]any{"type": "workspace"}})
	if code != 201 {
		t.Fatalf("message: %d %v", code, msg)
	}

	// 9. 越权校验：只读 credential 不能建 task（403 INSUFFICIENT_SCOPE）。
	code, viewer := do(t, "POST", base+"/workspaces/"+wsID+"/agents", humanAccess,
		map[string]any{"display_name": "Observer", "kind": "agent"})
	if code != 201 {
		t.Fatalf("create viewer agent: %d %v", code, viewer)
	}
	viewerID := viewer["id"].(string)
	code, vcred := do(t, "POST", base+"/agents/"+viewerID+"/credentials", humanAccess,
		map[string]any{"scopes": []string{"task:read"}, "workspace_id": wsID})
	if code != 201 {
		t.Fatalf("viewer credential: %d %v", code, vcred)
	}
	viewerSecret := vcred["secret"].(string)
	code, body := do(t, "POST", base+"/workspaces/"+wsID+"/children", viewerSecret,
		map[string]any{"title": "nope"})
	if code != 403 || errCode(t, body) != "INSUFFICIENT_SCOPE" {
		t.Fatalf("scope enforcement: %d %v", code, body)
	}
	// 无凭证访问受保护端点 → 401。
	code, body = do(t, "GET", base+"/workspaces/"+wsID, "", nil)
	if code != 401 {
		t.Fatalf("unauth ws get: %d %v", code, body)
	}
}

func TestRegisterBootstrapClosed(t *testing.T) {
	ts := newTestServer(t)
	base := ts.URL + "/api/v1"
	if code, _ := do(t, "POST", base+"/auth/register", "", map[string]any{
		"email": "a@example.com", "password": "hunter2safe",
	}); code != 201 {
		t.Fatalf("first register: %d", code)
	}
	code, body := do(t, "POST", base+"/auth/register", "", map[string]any{
		"email": "b@example.com", "password": "hunter2safe",
	})
	if code != 403 {
		t.Fatalf("second register: %d %v", code, body)
	}
}
