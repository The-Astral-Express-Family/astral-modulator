package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
)

// 公共请求/响应头（docs/protocol.md §2，未变）。
const (
	HeaderClient          = "X-Astral-Client"
	HeaderClientVersion   = "X-Astral-Client-Version"
	HeaderRequestID       = "X-Astral-Request-Id"
	HeaderProtocolVersion = "X-Astral-Protocol-Version"
	HeaderIdempotencyKey  = "Idempotency-Key"

	// ProtocolVersion 是当前公网协议大版本，与 /.well-known/astral 的
	// protocol_version 字段及 openapi.info.version 的 major 位一致。
	ProtocolVersion = 2
)

type ctxKeyRequestID struct{}

// RequestIDMiddleware 生成或透传 X-Astral-Request-Id，并在所有响应上回写
// X-Astral-Protocol-Version。CLI 依赖 request_id 做链路排查，
// 依赖 protocol header 做兼容判断，两者必须出现在每个 /api/v1 响应上。
func RequestIDMiddleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderRequestID)
		if reqID == "" {
			reqID = ids.New(ids.Request)
		}
		w.Header().Set(HeaderRequestID, reqID)
		w.Header().Set(HeaderProtocolVersion, strconv.Itoa(ProtocolVersion))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID{}, reqID)))
	}
	return http.HandlerFunc(fn)
}

// RequestIDFrom 取当前请求的 request id（含错误 envelope 里回填的场景）。
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID{}).(string); ok {
		return v
	}
	return ""
}

// Recover 把 panic 归一为 500 错误 envelope（契约层不允许任何响应绕过
// error.code 语义，包括 panic 路径）。已开始写响应体时只记日志，不再追加。
// http.ErrAbortHandler 按净跳过（与 net/http 约定一致）。
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
					log.Error("panic recovered",
						"err", rec,
						"request_id", RequestIDFrom(r.Context()),
						"method", r.Method, "path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					if ww.BytesWritten() == 0 {
						WriteError(ww, r, &APIError{
							Status:  http.StatusInternalServerError,
							Code:    CodeInternalError,
							Message: "internal error",
						})
					}
				}
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// Logger 输出结构化访问日志（method/path/status/耗时/request_id/client）。
// TODO(phase-6): 按部署文档接入 metrics/tracing；当前只保留 slog。
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
				"client", r.Header.Get(HeaderClient),
				"client_version", r.Header.Get(HeaderClientVersion),
			)
		}
		return http.HandlerFunc(fn)
	}
}

// CORS 仅供 Web GUI 开发期使用（Vite dev server 跨域直连后端）。
// 生产环境 Web 与 API 同源部署时不应配置任何允许来源。
// TODO(phase-6): 生产默认关闭；按部署文档确认是否需要凭据跨域。
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers",
					"Authorization, Content-Type, "+HeaderClient+", "+HeaderClientVersion+", "+HeaderRequestID+", "+HeaderIdempotencyKey+", Last-Event-ID")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}
