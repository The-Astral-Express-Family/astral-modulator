// Package idempotency 实现 Idempotency-Key 写操作去重（docs/protocol.md §4、
// architecture §21）：同一 Actor + endpoint + key 在保留窗口内重放首次 2xx 响应。
//
// 设计：
//   - 仅当客户端携带 Idempotency-Key 头时激活（契约是“支持”而非“要求”）；
//   - 挂载在鉴权之后：actor 身份来自 Principal，公共端点天然不受影响；
//   - 只缓存 2xx 响应（4xx/5xx 可安全重试执行）；超过 bodyCacheLimit 的
//     成功响应不缓存（执行照常）；
//   - 并发同键（R8）：进程内 per-key 互斥——同一 Actor + endpoint + key 的
//     并发请求串行化，业务 handler 只执行一次，后到者重放首到者的已存
//     响应。跨实例（多进程）窗口不在此保护范围（见 Handler 内注释）。
package idempotency

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/background"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
)

const (
	// RetentionWindow 幂等键保留时长；超窗后同 key 视为新请求。
	RetentionWindow = 24 * time.Hour
	// bodyCacheLimit 超过此大小的 2xx 响应体不缓存。
	bodyCacheLimit = 64 << 10 // 64KB
	// HeaderReplayed 标记该响应来自幂等重放。
	HeaderReplayed = "X-Astral-Idempotent-Replay"
)

type Middleware struct {
	DB  *gorm.DB
	Log *slog.Logger

	// locks 进程内并发同键互斥表（R8）：lockKey -> *sync.Mutex。
	// 零值可用；锁表只增不减——键空间 = actor × endpoint × 客户端 key，
	// 单个互斥锁对象极小，泄漏上界可接受（按引用计数清理得不偿失）。
	locks sync.Map
}

// wrappingResponseWriter 捕获状态码与响应体。
type wrappingResponseWriter struct {
	http.ResponseWriter
	status     int
	body       bytes.Buffer
	bodyTooBig bool
}

func (w *wrappingResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *wrappingResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.bodyTooBig {
		if w.body.Len()+len(b) > bodyCacheLimit {
			w.bodyTooBig = true
		} else {
			w.body.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(httpx.HeaderIdempotencyKey)
		if key == "" || r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if m.DB == nil {
			next.ServeHTTP(w, r)
			return
		}
		p := auth.PrincipalFrom(r.Context())
		if p == nil {
			next.ServeHTTP(w, r)
			return
		}
		endpoint := r.Method + " " + chiRoutePattern(r)
		now := time.Now()

		// R8：进程内同键互斥。进 handler 前拿锁、defer 释放（panic 路径
		// 同样经 defer 解锁，交由外层 Recover 归一 500）；拿锁后先查已存
		// 响应——并发同键只有首到者执行业务，后到者重放其 2xx 响应。
		// 边界：互斥仅在单进程内生效；多实例部署下跨进程同键并发仍会
		// 双执行，触发条件与迁移口径同 store/db.go:62 的既有
		// TODO(phase-6)（单实例 MVP）。
		mu := m.lockFor(p.ActorID + "\x00" + endpoint + "\x00" + key)
		mu.Lock()
		defer mu.Unlock()

		// 命中窗口内的已有响应 → 重放。
		if cached, ok := m.lookup(r, p.ActorID, endpoint, key, now); ok {
			replay(w, cached)
			return
		}

		ww := &wrappingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)

		// 只缓存 2xx。
		if ww.status < 200 || ww.status > 299 || ww.bodyTooBig {
			return
		}
		row := model.IdempotencyKey{
			ActorID:     p.ActorID,
			Endpoint:    endpoint,
			Key:         key,
			StatusCode:  ww.status,
			ContentType: ww.Header().Get("Content-Type"),
			Body:        ww.body.Bytes(),
			CreatedAt:   now,
		}
		if err := m.DB.Create(&row).Error; err != nil {
			// 跨实例并发同键（进程内互斥管不到的窗口）：主键冲突 →
			// 重读首到者的响应重放；读不到则放行本响应。
			if cached, ok := m.lookup(r, p.ActorID, endpoint, key, now); ok {
				replay(w, cached)
				return
			}
			m.Log.Warn("idempotency store failed", "err", err)
		}
	})
}

// lockFor 取（或惰性创建）进程内 per-key 互斥锁。键用 \x00 分隔，避免
// actor/endpoint/key 三段内容拼接产生歧义碰撞。
func (m *Middleware) lockFor(lockKey string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(lockKey, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// replay 写回缓存的 2xx 响应并标记重放头。
func replay(w http.ResponseWriter, c cachedResponse) {
	w.Header().Set("Content-Type", c.ContentType)
	w.Header().Set(HeaderReplayed, "true")
	w.WriteHeader(c.StatusCode)
	_, _ = w.Write(c.Body)
}

type cachedResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

func (m *Middleware) lookup(r *http.Request, actorID, endpoint, key string, now time.Time) (cachedResponse, bool) {
	var row model.IdempotencyKey
	err := m.DB.WithContext(r.Context()).
		Where("actor_id = ? AND endpoint = ? AND key = ? AND created_at > ?",
			actorID, endpoint, key, now.Add(-RetentionWindow)).
		First(&row).Error
	if err != nil {
		// miss 是正常路径；真实 DB 故障不能静默当 miss，记日志留排查线索。
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			m.Log.Warn("idempotency lookup failed", "err", err)
		}
		return cachedResponse{}, false
	}
	return cachedResponse{StatusCode: row.StatusCode, ContentType: row.ContentType, Body: row.Body}, true
}

// chiRoutePattern 取路由模板（如 "POST /workspaces"）而非具体路径，
// 保证同一路由的不同资源实例共享幂等键空间。
func chiRoutePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}

// StartCleanup 周期清理超窗幂等键。由 app 装配启动。
func StartCleanup(ctx context.Context, db *gorm.DB, log *slog.Logger, every time.Duration) {
	background.RunEvery(ctx, every, func(ctx context.Context) {
		res := db.WithContext(ctx).Where("created_at < ?", time.Now().Add(-RetentionWindow)).
			Delete(&model.IdempotencyKey{})
		if res.Error != nil {
			log.Error("idempotency cleanup failed", "err", res.Error)
			return
		}
		if res.RowsAffected > 0 {
			log.Info("idempotency keys cleaned", "rows", res.RowsAffected)
		}
	})
}
