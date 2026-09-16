package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeClock 是可推进的注入时钟：窗口恢复与惰性清理测试推进它。
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newReq 构造带直连地址（与可选 XFF）的请求；RemoteAddr 缺省 127.0.0.1。
func newReq(method, target, remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	}
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

// hit 经指定中间件打一次请求，返回 (状态码, Retry-After, 错误 envelope)。
func hit(t *testing.T, mw func(http.Handler) http.Handler, method, target, remoteAddr, xff string) (int, string, map[string]any) {
	t.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := newReq(method, target, remoteAddr, xff)
	req.Header.Set("X-Astral-Request-Id", "req_test")
	h.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, rec.Header().Get("Retry-After"), body
}

func errField(t *testing.T, body map[string]any, key string) any {
	t.Helper()
	if body == nil {
		t.Fatal("no body")
	}
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	return e[key]
}

func TestLimiterBucketFullAndRefill(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(2, clk.now) // 容量 2，1 枚/30s 回填

	for i := 0; i < 2; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("initial token %d denied", i)
		}
	}
	ok, wait := l.Allow("k")
	if ok || wait != 30*time.Second {
		t.Fatalf("want deny with 30s wait, got ok=%v wait=%v", ok, wait)
	}

	// 窗口恢复：推进过下一枚令牌恢复时刻后放行；差 1s 仍拒。
	//（恢复步进取 31s 而非精确 30s：回填是浮点累加，避免踩表示误差。）
	clk.advance(29 * time.Second)
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("denied prematurely at 29s")
	}
	clk.advance(2 * time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("should recover after full interval")
	}
}

func TestLimiterKeysIsolated(t *testing.T) {
	l := NewLimiter(1, newFakeClock().now)
	if ok, _ := l.Allow("ip:a"); !ok {
		t.Fatal("first key denied")
	}
	if ok, _ := l.Allow("ip:b"); !ok {
		t.Fatal("second key must not share bucket")
	}
}

func TestLimiterDisabledPassesAll(t *testing.T) {
	l := NewLimiter(0, newFakeClock().now) // PerMin<=0 = 禁用
	for i := 0; i < 100; i++ {
		if ok, wait := l.Allow("k"); !ok || wait != 0 {
			t.Fatalf("disabled bucket denied at %d", i)
		}
	}
	var nilLimiter *Limiter
	if ok, _ := nilLimiter.Allow("k"); !ok {
		t.Fatal("nil limiter must pass")
	}
}

func TestLazyCleanupSweepsIdleKeys(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(10, clk.now)
	l.Allow("ip:a")
	l.Allow("ip:b")
	if got := l.Len(); got != 2 {
		t.Fatalf("want 2 keys, got %d", got)
	}

	// 未超 idleTTL 不清理（推进 5min，中间有请求触发扫描节流窗口）。
	clk.advance(5 * time.Minute)
	l.Allow("ip:c")
	if got := l.Len(); got != 3 {
		t.Fatalf("premature cleanup: want 3 keys, got %d", got)
	}

	// 超 10min 空闲：下次 Allow 触发扫描，a/b/c 全部回收，仅留新 key。
	clk.advance(11 * time.Minute)
	l.Allow("ip:d")
	if got := l.Len(); got != 1 {
		t.Fatalf("lazy cleanup failed: want 1 key, got %d", got)
	}
	if ok, _ := l.Allow("ip:a"); !ok {
		t.Fatal("recreated key should get a fresh full bucket")
	}
}

func TestPublicSensitiveBucket429WithRetryAfter(t *testing.T) {
	clk := newFakeClock()
	m := New(Config{SensitivePerMin: 1, Now: clk.now}, nil)

	// 第 1 发放行；第 2 发 429 RATE_LIMITED + Retry-After=60
	//（1 枚/60s 回填 → 下一枚令牌在 60s 后）。
	code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "9.9.9.9:1", "")
	if code != http.StatusOK {
		t.Fatalf("first login: %d", code)
	}
	code, retryAfter, body := hit(t, m.Public, "POST", "/api/v1/auth/login", "9.9.9.9:2", "")
	if code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", code)
	}
	if retryAfter != "60" {
		t.Fatalf("Retry-After: want 60, got %q", retryAfter)
	}
	if c := errField(t, body, "code"); c != "RATE_LIMITED" {
		t.Fatalf("error.code: %v", c)
	}
	if r := errField(t, body, "retryable"); r != true {
		t.Fatalf("error.retryable: %v", r)
	}
	if id := errField(t, body, "request_id"); id != "req_test" {
		t.Fatalf("request_id passthrough: %v", id)
	}

	// 窗口恢复：推进 60s 后放行（注入时钟验证，不等真实时间）。
	clk.advance(60 * time.Second)
	if code, _, _ = hit(t, m.Public, "POST", "/api/v1/auth/login", "9.9.9.9:3", ""); code != http.StatusOK {
		t.Fatalf("after window: %d", code)
	}
}

func TestPublicBucketsIsolated(t *testing.T) {
	clk := newFakeClock()
	m := New(Config{SensitivePerMin: 1, PollPerMin: 5, APIPerMin: 5, Now: clk.now}, nil)

	// 打满敏感桶（login）。
	if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "8.8.8.8:1", ""); code != http.StatusOK {
		t.Fatal("first login denied")
	}
	if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "8.8.8.8:2", ""); code != http.StatusTooManyRequests {
		t.Fatal("sensitive bucket not exhausted")
	}

	// 轮询桶不受影响：device token 交换仍放行（同 IP）。
	if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/device/authorizations/adc_x/token", "8.8.8.8:3", ""); code != http.StatusOK {
		t.Fatalf("poll bucket affected by sensitive exhaustion: %d", code)
	}
	// 通用桶（公共面按 IP）不受影响：logout 仍放行。
	if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/logout", "8.8.8.8:4", ""); code != http.StatusOK {
		t.Fatalf("api bucket affected by sensitive exhaustion: %d", code)
	}
	// 受保护路径在 Public 段放行不记账（哪怕所有桶都满）。
	if code, _, _ := hit(t, m.Public, "GET", "/api/v1/workspaces/ws_1", "8.8.8.8:5", ""); code != http.StatusOK {
		t.Fatalf("protected path must pass through Public: %d", code)
	}
}

func TestXFFTrustedSwitch(t *testing.T) {
	t.Run("untrusted_ignores_xff", func(t *testing.T) {
		clk := newFakeClock()
		m := New(Config{SensitivePerMin: 1, TrustedProxy: false, Now: clk.now}, nil)
		// 不同 XFF、相同直连地址 → 同一 key（RemoteAddr），第 2 发 429。
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:1", "1.2.3.4"); code != http.StatusOK {
			t.Fatal("first denied")
		}
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:2", "5.6.7.8"); code != http.StatusTooManyRequests {
			t.Fatalf("untrusted XFF must not split buckets: %d", code)
		}
	})
	t.Run("trusted_uses_first_hop", func(t *testing.T) {
		clk := newFakeClock()
		m := New(Config{SensitivePerMin: 1, TrustedProxy: true, Now: clk.now}, nil)
		// 相同直连地址、不同 XFF 首跳 → 不同 key，各自放行。
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:1", "1.2.3.4, 10.0.0.1"); code != http.StatusOK {
			t.Fatal("first denied")
		}
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:2", "5.6.7.8, 10.0.0.1"); code != http.StatusOK {
			t.Fatalf("distinct XFF first hop must get own bucket: %d", code)
		}
		// 同 XFF 首跳 → 同 key，第 2 发 429。
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:3", "1.2.3.4"); code != http.StatusTooManyRequests {
			t.Fatalf("same XFF first hop must share bucket: %d", code)
		}
		// XFF 缺失 → 回退 RemoteAddr。
		if code, _, _ := hit(t, m.Public, "POST", "/api/v1/auth/login", "7.7.7.7:4", ""); code != http.StatusOK {
			t.Fatalf("missing XFF must fall back to RemoteAddr: %d", code)
		}
	})
}

func TestPrivateActorKeyedBuckets(t *testing.T) {
	clk := newFakeClock()
	actor := "act_1"
	m := New(Config{SensitivePerMin: 10, APIPerMin: 1, SSEPerMin: 1, Now: clk.now},
		func(*http.Request) string { return actor })

	// SSE 桶独立于通用桶：打满 events（1/min）后通用路径仍放行。
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1/events", "6.6.6.6:1", ""); code != http.StatusOK {
		t.Fatal("first events denied")
	}
	if code, retry, body := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1/events", "6.6.6.6:2", ""); code != http.StatusTooManyRequests {
		t.Fatalf("SSE bucket not enforced: %d", code)
	} else if retry != "60" || errField(t, body, "code") != "RATE_LIMITED" {
		t.Fatalf("SSE 429 shape: retry=%q body=%v", retry, body)
	}
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1", "6.6.6.6:3", ""); code != http.StatusOK {
		t.Fatalf("SSE storm must not consume api bucket: %d", code)
	}

	// 通用桶按 actor 记账：act_1 已耗尽（上面 1 枚），换 actor 放行。
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1", "6.6.6.6:4", ""); code != http.StatusTooManyRequests {
		t.Fatal("api bucket per actor not enforced")
	}
	actor = "act_2"
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1", "6.6.6.6:5", ""); code != http.StatusOK {
		t.Fatalf("distinct actor must get own api bucket: %d", code)
	}
	// actor 不可得（actorFrom 空）→ 回退 IP key。
	actor = ""
	clk.advance(time.Minute) // 让 act_* 与 ip 桶都回填，隔离验证 ip 维度
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1", "6.6.6.6:6", ""); code != http.StatusOK {
		t.Fatalf("IP fallback key denied: %d", code)
	}
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/workspaces/ws_1", "6.6.6.6:7", ""); code != http.StatusTooManyRequests {
		t.Fatal("IP fallback key must accumulate")
	}
}

func TestPrivateDeviceApprovalSensitiveByIP(t *testing.T) {
	clk := newFakeClock()
	actor := "act_1"
	m := New(Config{SensitivePerMin: 1, APIPerMin: 10, Now: clk.now},
		func(*http.Request) string { return actor })

	// GET /auth/device/authorizations（user_code 防枚举）入敏感桶、按 IP：
	// 换 actor 也拒（第 2 发），证明 key 不随身份走。
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/auth/device/authorizations?user_code=AAAA-BBBB", "5.5.5.5:1", ""); code != http.StatusOK {
		t.Fatal("first approval query denied")
	}
	actor = "act_2"
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/auth/device/authorizations?user_code=CCCC-DDDD", "5.5.5.5:2", ""); code != http.StatusTooManyRequests {
		t.Fatalf("approval query must be IP-keyed: %d", code)
	}
	// approve/deny 同桶（同 IP 第 3 发 429）。
	if code, _, _ := hit(t, m.Private, "POST", "/api/v1/auth/device/authorizations/appr_1/approve", "5.5.5.5:3", ""); code != http.StatusTooManyRequests {
		t.Fatalf("approve must share sensitive bucket: %d", code)
	}
	// 不同 IP 不受连坐。
	if code, _, _ := hit(t, m.Private, "POST", "/api/v1/auth/device/authorizations/appr_1/deny", "4.4.4.4:1", ""); code != http.StatusOK {
		t.Fatalf("different IP must not be affected: %d", code)
	}
	// /auth/me 等其余 auth 端点入通用桶（不受敏感桶打满影响）。
	if code, _, _ := hit(t, m.Private, "GET", "/api/v1/auth/me", "5.5.5.5:4", ""); code != http.StatusOK {
		t.Fatalf("me must use api bucket: %d", code)
	}
}

func TestClassifyTables(t *testing.T) {
	cases := []struct {
		name         string
		method, path string
		pub, priv    bucketClass
	}{
		{"login", "POST", "/api/v1/auth/login", classSensitive, classAPI},
		{"register", "POST", "/api/v1/auth/register", classSensitive, classAPI},
		{"refresh", "POST", "/api/v1/auth/token/refresh", classSensitive, classAPI},
		{"device_create", "POST", "/api/v1/auth/device/authorizations", classSensitive, classAPI},
		{"device_get", "GET", "/api/v1/auth/device/authorizations", classNone, classSensitive},
		{"approve", "POST", "/api/v1/auth/device/authorizations/appr_1/approve", classNone, classSensitive},
		{"deny", "POST", "/api/v1/auth/device/authorizations/appr_1/deny", classNone, classSensitive},
		{"device_token_poll", "POST", "/api/v1/auth/device/authorizations/adc_1/token", classPoll, classAPI},
		{"logout", "POST", "/api/v1/auth/logout", classAPI, classAPI},
		{"me", "GET", "/api/v1/auth/me", classNone, classAPI},
		{"capabilities", "GET", "/api/v1/meta/capabilities", classAPI, classAPI},
		{"events", "GET", "/api/v1/workspaces/ws_1/events", classNone, classSSE},
		{"workspace", "GET", "/api/v1/workspaces/ws_1", classNone, classAPI},
		{"task_create", "POST", "/api/v1/workspaces/ws_1/children", classNone, classAPI},
		{"login_wrong_method", "GET", "/api/v1/auth/login", classNone, classAPI},
		{"unknown", "POST", "/api/v1/nope", classNone, classAPI},
	}
	for _, c := range cases {
		if got := classifyPublic(c.method, c.path); got != c.pub {
			t.Errorf("%s: classifyPublic=%v want %v", c.name, got, c.pub)
		}
		if got := classifyPrivate(c.method, c.path); got != c.priv {
			t.Errorf("%s: classifyPrivate=%v want %v", c.name, got, c.priv)
		}
	}
}
