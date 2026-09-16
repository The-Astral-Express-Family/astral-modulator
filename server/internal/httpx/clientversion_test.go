package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientVersionMiddleware 锁定 R2 三态：无头放行 / 合法头放行 /
// 非法或过旧头 400 CLIENT_VERSION_UNSUPPORTED。
func TestClientVersionMiddleware(t *testing.T) {
	cases := []struct {
		name       string
		version    string // "" = 头缺失
		wantPass   bool
		wantStatus int
	}{
		{"absent header passes", "", true, http.StatusOK},
		{"minimum accepted version passes", "2", true, http.StatusOK},
		{"newer version passes", "3", true, http.StatusOK},
		{"too old rejected", "1", false, http.StatusBadRequest},
		{"zero rejected", "0", false, http.StatusBadRequest},
		{"negative rejected", "-1", false, http.StatusBadRequest},
		{"non-integer rejected", "0.1.0", false, http.StatusBadRequest},
		{"garbage rejected", "abc", false, http.StatusBadRequest},
		{"whitespace rejected", " 2", false, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
			if c.version != "" {
				req.Header.Set(HeaderClientVersion, c.version)
			}
			ClientVersionMiddleware(next).ServeHTTP(rec, req)

			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if reached != c.wantPass {
				t.Fatalf("downstream reached = %v, want %v", reached, c.wantPass)
			}
			if c.wantPass {
				return
			}
			var env struct {
				Error struct {
					Code      string `json:"code"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("invalid error envelope: %v", err)
			}
			if env.Error.Code != CodeClientVersionUnsupported {
				t.Fatalf("error.code = %q, want %q", env.Error.Code, CodeClientVersionUnsupported)
			}
			if env.Error.Retryable {
				t.Fatal("client version error must not be retryable")
			}
		})
	}
}
