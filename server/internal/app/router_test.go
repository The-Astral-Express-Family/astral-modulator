// Package app 的路由冒烟测试：不依赖数据库即可验证 CLI 依赖的公共契约——
// well-known、capabilities、请求头、错误 envelope、501 桩形状。
// astral-cli 仓库的 contract test 应以 api/openapi.yaml 为准；
// 本文件防的是服务端自身回归。
package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewRouter(config.Config{ServerID: "srv_test", PublicURL: "https://astral.example.com"}, log, nil, event.NewHub())
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestWellKnown(t *testing.T) {
	ts := testServer(t)

	resp, err := http.Get(ts.URL + "/.well-known/astral")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var wk WellKnown
	if err := json.NewDecoder(resp.Body).Decode(&wk); err != nil {
		t.Fatal(err)
	}
	if wk.ServerID != "srv_test" || wk.APIBase != "/api/v1" || wk.ProtocolVersion != 1 {
		t.Errorf("unexpected well-known payload: %+v", wk)
	}
	if !wk.Auth.DeviceLogin {
		t.Error("device_login should be true in v0.1")
	}
	if resp.Header.Get("X-Astral-Protocol-Version") != "1" {
		t.Error("protocol version header missing on well-known")
	}
}

func TestCapabilities(t *testing.T) {
	ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/meta/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var caps Capabilities
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		t.Fatal(err)
	}
	if caps.ProtocolVersion != 1 || caps.MinimumCliVersion == "" || caps.Features == nil {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}

func TestStubEndpointReturnsEnvelope(t *testing.T) {
	ts := testServer(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workspaces/ws_x/tasks", nil)
	req.Header.Set("X-Astral-Request-Id", "req_smoke_1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
	if resp.Header.Get("X-Astral-Request-Id") != "req_smoke_1" {
		t.Error("request id not echoed")
	}
	var env struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "NOT_IMPLEMENTED" || env.Error.RequestID != "req_smoke_1" {
		t.Errorf("unexpected stub envelope: %+v", env)
	}
}

func TestUnknownAPIPathReturnsEnvelope(t *testing.T) {
	ts := testServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("404 body is not json envelope: %v", err)
	}
	if _, ok := env["error"]; !ok {
		t.Error("404 body missing error envelope")
	}
}

func TestReadyzWithoutDB(t *testing.T) {
	ts := testServer(t)

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
