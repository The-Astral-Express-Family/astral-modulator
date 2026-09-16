package idempotency

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

func newTestMiddleware(t *testing.T) *Middleware {
	t.Helper()
	return &Middleware{
		DB:  testsupport.NewTestDB(t),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// keyedRequest 构造带 principal 与 Idempotency-Key 的写请求（中间件挂在
// 鉴权之后，测试直接注入 Principal；无 chi 路由上下文时 endpoint 回退为
// 具体路径，同一 path 即同一键空间）。
func keyedRequest(t *testing.T, method, path, key string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if key != "" {
		r.Header.Set(httpx.HeaderIdempotencyKey, key)
	}
	return r.WithContext(auth.WithPrincipal(r.Context(), &auth.Principal{
		ActorID: "usr_idem1", Kind: "human", AuthKind: "access_token",
	}))
}

func TestHandlerPassthroughWithoutKey(t *testing.T) {
	m := newTestMiddleware(t)
	var calls atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	h := m.Handler(next)

	// 无 key、GET 带 key：都不激活幂等逻辑。
	h.ServeHTTP(httptest.NewRecorder(), keyedRequest(t, http.MethodPost, "/api/v1/tasks", ""))
	h.ServeHTTP(httptest.NewRecorder(), keyedRequest(t, http.MethodGet, "/api/v1/tasks", "k1"))
	if calls.Load() != 2 {
		t.Fatalf("handler calls = %d, want 2 (both passthrough)", calls.Load())
	}
}

func TestReplayCachedResponse(t *testing.T) {
	m := newTestMiddleware(t)
	var calls atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tsk_1"}`))
	})
	h := m.Handler(next)

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, keyedRequest(t, http.MethodPost, "/api/v1/tasks", "k1"))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, keyedRequest(t, http.MethodPost, "/api/v1/tasks", "k1"))

	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
	if w1.Code != http.StatusCreated || w2.Code != http.StatusCreated {
		t.Fatalf("status: first=%d second=%d, want 201/201", w1.Code, w2.Code)
	}
	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("replayed body %q != original %q", w2.Body.String(), w1.Body.String())
	}
	if w1.Header().Get(HeaderReplayed) != "" {
		t.Error("first response must not carry replay header")
	}
	if w2.Header().Get(HeaderReplayed) != "true" {
		t.Error("second response must carry replay header")
	}

	// 不同 key = 不同幂等键空间：再次执行，无重放头。
	var calls2 atomic.Int32
	h2 := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls2.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	w3 := httptest.NewRecorder()
	h2.ServeHTTP(w3, keyedRequest(t, http.MethodPost, "/api/v1/tasks", "k2"))
	if calls2.Load() != 1 || w3.Header().Get(HeaderReplayed) != "" {
		t.Fatalf("different key: calls=%d replay=%q, want 1/no-header", calls2.Load(), w3.Header().Get(HeaderReplayed))
	}
}

// TestConcurrentSameKeySingleExecution 锁定 R8：并发同键请求在进程内互斥，
// 业务 handler 只执行一次，全部响应与首到者一致。
func TestConcurrentSameKeySingleExecution(t *testing.T) {
	m := newTestMiddleware(t)
	var calls atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// 拉大执行窗口：未互斥时并发各方都会越过 lookup miss 进入 handler。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tsk_1"}`))
	})
	h := m.Handler(next)

	const n = 8
	type result struct {
		status int
		body   string
		replay bool
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			<-start
			h.ServeHTTP(w, keyedRequest(t, http.MethodPost, "/api/v1/tasks", "k-race"))
			results[i] = result{
				status: w.Code,
				body:   w.Body.String(),
				replay: w.Header().Get(HeaderReplayed) == "true",
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("handler executed %d times under concurrency, want exactly 1", calls.Load())
	}
	for i, res := range results {
		if res.status != http.StatusCreated {
			t.Errorf("response %d: status = %d, want 201", i, res.status)
		}
		if res.body != results[0].body {
			t.Errorf("response %d: body = %q, want %q (identical to first)", i, res.body, results[0].body)
		}
	}
	replays := 0
	for _, res := range results {
		if res.replay {
			replays++
		}
	}
	if replays != n-1 {
		t.Fatalf("replay headers = %d, want %d (first responder excluded)", replays, n-1)
	}
}
