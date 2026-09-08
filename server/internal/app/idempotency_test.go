package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// TestIdempotencyKeyReplay：同 actor + endpoint + key 重放首次 2xx 响应
// （含 X-Astral-Idempotent-Replay 标记）；不同 key 独立执行；
// 无凭证请求（401）不缓存。
func TestIdempotencyKeyReplay(t *testing.T) {
	ts := newTestServer(t)
	base := ts.URL + "/api/v1"

	// bootstrap 注册 + Cookie 登录。
	regBody, _ := json.Marshal(map[string]any{
		"email": "idem@example.com", "password": "hunter2safe",
	})
	resp, err := http.Post(base+"/auth/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	{
		loginBody, _ := json.Marshal(map[string]any{
			"email": "idem@example.com", "password": "hunter2safe",
		})
		resp, err := http.Post(base+"/auth/login", "application/json", bytes.NewReader(loginBody))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "astral_session" {
				cookie = c
			}
		}
		resp.Body.Close()
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}

	createWithKey := func(key string) (string, map[string]any) {
		req, _ := http.NewRequest("POST", base+"/workspaces", bytes.NewReader([]byte(`{"name":"demo"}`)))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.Header.Get("X-Astral-Idempotent-Replay"), out
	}

	// 第一次：真实执行。
	replay1, ws1 := createWithKey("key-abc")
	if replay1 != "" {
		t.Fatal("first execution must not be marked as replay")
	}
	wsID, _ := ws1["id"].(string)
	if wsID == "" {
		t.Fatalf("workspace create failed: %+v", ws1)
	}

	// 同 key 重试：重放同一响应（同一 workspace id），带重放标记。
	replay2, ws2 := createWithKey("key-abc")
	if replay2 != "true" {
		t.Fatalf("second request should be replayed (header=%q)", replay2)
	}
	if ws2["id"] != wsID {
		t.Fatalf("replayed response mismatch: %v vs %v", ws2["id"], wsID)
	}

	// 不同 key：独立执行（slug 冲突 → 409；验证确实执行了新请求）。
	_, ws3 := createWithKey("key-other")
	if ws3["error"] == nil {
		t.Fatalf("different key should execute for real (slug conflict expected), got %+v", ws3)
	}

	// 未鉴权 + key：401 错误响应不缓存，两次都真实执行且无重放标记。
	unauth := func(key string) http.Header {
		req, _ := http.NewRequest("POST", base+"/workspaces", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("want 401, got %d", resp.StatusCode)
		}
		return resp.Header
	}
	if h := unauth("key-unauth"); h.Get("X-Astral-Idempotent-Replay") != "" {
		t.Fatal("error responses must not be cached")
	}
	if h := unauth("key-unauth"); h.Get("X-Astral-Idempotent-Replay") != "" {
		t.Fatal("error responses must not be replayed")
	}
}
