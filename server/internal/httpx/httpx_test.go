package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestErrorEnvelopeShape 锁定错误 envelope 的 JSON 形状（protocol §4）。
// CLI 的错误处理直接解析这些字段，形状变化即协议破坏。
func TestErrorEnvelopeShape(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws_x", nil)
	r.Header.Set(HeaderRequestID, "req_test123")
	w := httptest.NewRecorder()

	WriteError(w, r, &APIError{
		Status:  http.StatusConflict,
		Code:    CodeTaskAlreadyClaimed,
		Message: "Task is already claimed by another actor",
		Details: map[string]any{"task_id": "tsk_1", "holder_actor_id": "agt_7"},
	})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if got := w.Header().Get("X-Astral-Request-Id"); got != "req_test123" {
		t.Errorf("request id header not echoed: %q", got)
	}

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	e, ok := raw["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %v", raw)
	}
	for _, key := range []string{"code", "message", "retryable", "request_id"} {
		if _, ok := e[key]; !ok {
			t.Errorf("error envelope missing key %q", key)
		}
	}
	if e["code"] != CodeTaskAlreadyClaimed {
		t.Errorf("code = %v", e["code"])
	}
	if e["request_id"] != "req_test123" {
		t.Errorf("request_id = %v", e["request_id"])
	}
	if _, ok := e["details"]; !ok {
		t.Error("details should be present when provided")
	}
}

func TestRetryableDefaults(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{CodeRateLimited, true},
		{CodeInternalError, true},
		{CodeTaskAlreadyClaimed, false},
		{CodeAuthRequired, false},
	}
	for _, c := range cases {
		got := (&APIError{Code: c.code}).IsRetryable()
		if got != c.want {
			t.Errorf("%s retryable = %v, want %v", c.code, got, c.want)
		}
	}
}
