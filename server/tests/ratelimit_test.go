// ratelimit_test.go 是 S5 限流的 app 级集成冒烟测试（round 38 T8）：
// 经真实 router 装配验证四个桶确实挂载生效——敏感桶 429+Retry-After、
// 轮询桶不受敏感桶打满连坐、SSE 桶独立于通用桶、通用桶按 actor 记账。
// 桶参数走 Config 注入的小额度（生产默认值的语义等价缩放）；零值 Config
// = 全禁用的免限流路径由其余既有集成测试隐式覆盖。
package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/app"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/admin"
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

// newRateLimitServer 起启用限流的完整 app：敏感 2/min、轮询 10/min、
// 通用 10/min、SSE 1/min（额度小便于快速打满；桶间比例与生产默认一致）。
func newRateLimitServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, log)
	hub := event.NewHub()
	mods := &app.Modules{
		Auth:      &auth.Module{Svc: svc, PublicURL: "https://astral.example.com"},
		Workspace: &workspace.Module{DB: db, Auth: svc},
		Task:      &task.Module{DB: db, Auth: svc},
		Tag:       &tag.Module{Auth: svc},
		Memory:    &memory.Module{},
		Document:  &document.Module{DB: db, Auth: svc},
		Message:   &message.Module{DB: db, Auth: svc},
		Presence:  &presence.Module{DB: db, Auth: svc},
		Audit:     &audit.Module{},
		Admin:     &admin.Module{DB: db, Auth: svc, Log: log},
		Events:    &event.SSEHandler{Hub: hub, DB: db, Auth: svc},
	}
	mods.Message.Tasks = mods.Task
	ts := httptest.NewServer(app.NewRouter(config.Config{
		ServerID:  "srv_rltest",
		PublicURL: "https://astral.example.com",
		RateLimit: config.RateLimitConfig{
			SensitivePerMin: 2,
			PollPerMin:      10,
			APIPerMin:       10,
			SSEPerMin:       1,
		},
	}, log, db, mods))
	t.Cleanup(ts.Close)
	// 通用/SSE 桶验证用主体（非 events workspace 成员 → 404 快速返回，
	// 不进入长连接流）。
	token := humanWithSession(t, db, "usr_rl_1", "user")
	return ts, token
}

// rlDo 发送 JSON/空请求并解析响应，返回状态码、Retry-After 与 error.code。
func rlDo(t *testing.T, ts *httptest.Server, method, path, bearer string, body []byte) (int, string, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, resp.Header.Get("Retry-After"), env.Error.Code
}

func TestRateLimitWiredThroughRouter(t *testing.T) {
	ts, token := newRateLimitServer(t)
	base := "/api/v1"
	loginBody := []byte(`{"email":"nobody@example.com","password":"wrong"}`)

	// 1. 敏感桶（2/min/IP）：两次 401（凭证错误，但已消费令牌），第三次
	//    429 RATE_LIMITED + Retry-After=30（2/min → 一枚令牌 30s 恢复）。
	for i := 1; i <= 2; i++ {
		if code, _, ec := rlDo(t, ts, "POST", base+"/auth/login", "", loginBody); code != 401 || ec != "AUTH_REQUIRED" {
			t.Fatalf("login #%d: want 401 AUTH_REQUIRED, got %d %q", i, code, ec)
		}
	}
	if code, retry, ec := rlDo(t, ts, "POST", base+"/auth/login", "", loginBody); code != 429 || ec != "RATE_LIMITED" || retry != "30" {
		t.Fatalf("login #3: want 429 RATE_LIMITED Retry-After=30, got %d %q retry=%q", code, ec, retry)
	}

	// 2. 轮询桶不受敏感桶连坐：同 IP 的 device token 交换仍走到业务层
	//    （未知 device_code → 401，而非 429）。
	if code, _, ec := rlDo(t, ts, "POST", base+"/auth/device/authorizations/adc_unknown/token", "", nil); code != 401 || ec != "AUTH_REQUIRED" {
		t.Fatalf("poll after sensitive exhausted: want 401 AUTH_REQUIRED, got %d %q", code, ec)
	}

	// 3. SSE 桶独立于通用桶：events 连接建立按 actor 记 1/min——首次
	//    过限流进入授权层（非成员 404），第二次 429 Retry-After=60。
	if code, _, ec := rlDo(t, ts, "GET", base+"/workspaces/ws_rl_none/events", token, nil); code != 404 || ec != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("events #1: want 404 WORKSPACE_NOT_FOUND, got %d %q", code, ec)
	}
	if code, retry, ec := rlDo(t, ts, "GET", base+"/workspaces/ws_rl_none/events", token, nil); code != 429 || ec != "RATE_LIMITED" || retry != "60" {
		t.Fatalf("events #2: want 429 RATE_LIMITED Retry-After=60, got %d %q retry=%q", code, ec, retry)
	}

	// 4. 通用桶（10/min/actor）：SSE 打满不影响通用桶（首个 me 200）；
	//    第 11 次 me 429 Retry-After=6（10/min → 一枚令牌 6s 恢复）。
	for i := 1; i <= 10; i++ {
		if code, _, _ := rlDo(t, ts, "GET", base+"/auth/me", token, nil); code != 200 {
			t.Fatalf("me #%d: want 200, got %d", i, code)
		}
	}
	if code, retry, ec := rlDo(t, ts, "GET", base+"/auth/me", token, nil); code != 429 || ec != "RATE_LIMITED" || retry != "6" {
		t.Fatalf("me #11: want 429 RATE_LIMITED Retry-After=6, got %d %q retry=%q", code, ec, retry)
	}
}
