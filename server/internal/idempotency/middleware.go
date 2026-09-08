// Package idempotency 实现 Idempotency-Key 写操作去重（docs/protocol.md §4、
// architecture §21）：同一 Actor + endpoint + key 在保留窗口内重放首次 2xx 响应。
//
// 设计：
//   - 仅当客户端携带 Idempotency-Key 头时激活（契约是“支持”而非“要求”）；
//   - 挂载在鉴权之后：actor 身份来自 Principal，公共端点天然不受影响；
//   - 只缓存 2xx 响应（4xx/5xx 可安全重试执行）；超过 bodyCacheLimit 的
//     成功响应不缓存（执行照常）；
//   - 并发同键：DB 主键兜底，后到者重读首到者已存响应。
package idempotency

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

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

		// 命中窗口内的已有响应 → 重放。
		if cached, ok := m.lookup(r, p.ActorID, endpoint, key, now); ok {
			w.Header().Set("Content-Type", cached.ContentType)
			w.Header().Set(HeaderReplayed, "true")
			w.WriteHeader(cached.StatusCode)
			_, _ = w.Write(cached.Body)
			return
		}

		ww := &wrappingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(ww, r)

		// 只缓存 2xx。
		if ww.status < 200 || ww.status > 299 || ww.bodyTooBig || ww.status == 0 {
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
			// 并发同键：主键冲突 → 重读首到者的响应重放；读不到则放行本响应。
			if cached, ok := m.lookup(r, p.ActorID, endpoint, key, now); ok {
				w.Header().Set("Content-Type", cached.ContentType)
				w.Header().Set(HeaderReplayed, "true")
				w.WriteHeader(cached.StatusCode)
				_, _ = w.Write(cached.Body)
				return
			}
			m.Log.Warn("idempotency store failed", "err", err)
		}
	})
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
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				res := db.Where("created_at < ?", time.Now().Add(-RetentionWindow)).
					Delete(&model.IdempotencyKey{})
				if res.Error != nil {
					log.Error("idempotency cleanup failed", "err", res.Error)
					continue
				}
				if res.RowsAffected > 0 {
					log.Info("idempotency keys cleaned", "rows", res.RowsAffected)
				}
			}
		}
	}()
}
