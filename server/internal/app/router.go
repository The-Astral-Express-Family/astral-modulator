// Package app 装配 HTTP 路由：公共中间件、发现端点、健康检查、
// 鉴权边界与各业务模块。新模块一律通过模块自身的 Register* 挂载到 /api/v1，
// 不得自起 http.Server 或绕过中间件。
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/idempotency"
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
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// WellKnown 是 GET /.well-known/astral 的响应（architecture §7）。
// 登录前可匿名访问，因此绝不包含部署敏感信息。
type WellKnown struct {
	ServerID              string `json:"server_id"`
	CanonicalURL          string `json:"canonical_url"`
	APIBase               string `json:"api_base"`
	ProtocolVersion       int    `json:"protocol_version"`
	MinCLIProtocolVersion int    `json:"min_cli_protocol_version"`
	Auth                  struct {
		DeviceLogin bool `json:"device_login"`
	} `json:"auth"`
}

// Capabilities 是 GET /api/v1/meta/capabilities 的响应（docs/protocol.md §7）。
type Capabilities struct {
	ProtocolVersion   int      `json:"protocol_version"`
	MinimumCliVersion string   `json:"minimum_cli_version"`
	Features          []string `json:"features"`
}

// Modules 汇集装配好的模块（由 main 构造，测试可用 sqlite 内存库构造）。
type Modules struct {
	// Idempotency 为写操作幂等中间件（nil 则不启用）。
	Idempotency *idempotency.Middleware

	Auth      *auth.Module
	Workspace *workspace.Module
	Task      *task.Module
	Tag       *tag.Module
	Memory    *memory.Module
	Document  *document.Module
	Message   *message.Module
	Presence  *presence.Module
	Audit     *audit.Module
	Admin     *admin.Module
	Events    *event.SSEHandler
}

// NewRouter 构建完整 http.Handler。db 为 nil 表示无数据库开发模式。
func NewRouter(cfg config.Config, log *slog.Logger, db *gorm.DB, mods *Modules) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.Recover(log))
	r.Use(httpx.Logger(log))
	r.Use(httpx.RequestIDMiddleware)
	if len(cfg.DevCORSOrigins) > 0 {
		r.Use(httpx.CORS(cfg.DevCORSOrigins))
	}

	// 发现与运维端点（无认证，不泄露内部信息）。
	r.Get("/.well-known/astral", wellKnownHandler(cfg, log))
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		httpx.WriteOK(w, req, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})
	r.Get("/readyz", readyzHandler(db))

	r.Route("/api/v1", func(api chi.Router) {
		api.NotFound(func(w http.ResponseWriter, req *http.Request) {
			httpx.WriteError(w, req, httpx.NotFound("no such endpoint under /api/v1"))
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
			httpx.WriteError(w, req, &httpx.APIError{
				Status:  http.StatusMethodNotAllowed,
				Code:    httpx.CodeValidationFailed,
				Message: "method not allowed",
			})
		})

		api.Get("/meta/capabilities", func(w http.ResponseWriter, req *http.Request) {
			// features 按实现进度逐步开放；未列出即不可用（docs/protocol.md §7）。
			// task_lease：原子 claim + lease renew/release 已实装（见 task 模块测试）。
			// document_sync：documents/conflicts 七端点已实装（round 38 T4；
			// memory 无独立 feature——复用 documents 端点，M1 裁决）。
			httpx.WriteOK(w, req, http.StatusOK, Capabilities{
				ProtocolVersion:   httpx.ProtocolVersion,
				MinimumCliVersion: "0.1.0",
				Features:          []string{"task_lease", "document_sync"},
			})
		})

		// 公共 auth 端点（免鉴权；device create/exchange、register/login/refresh/logout）。
		mods.Auth.RegisterPublic(api)

		// 受保护 API。TODO: 认证/授权失败写 audit（集中登记见 audit/module.go）。
		api.Group(func(priv chi.Router) {
			priv.Use(mods.Auth.Svc.Authenticate)
			// 幂等：挂载于鉴权后（actor 身份参与键空间）；仅当客户端携带
			// Idempotency-Key 头时激活。contract 列出的写端点全部受益。
			if mods.Idempotency != nil {
				priv.Use(mods.Idempotency.Handler)
			}
			mods.Auth.RegisterPrivate(priv)
			mods.Workspace.RegisterRoutes(priv)
			mods.Task.RegisterRoutes(priv)
			mods.Tag.RegisterRoutes(priv)
			mods.Memory.RegisterRoutes(priv)
			mods.Document.RegisterRoutes(priv)
			mods.Message.RegisterRoutes(priv)
			mods.Presence.RegisterRoutes(priv)
			mods.Audit.RegisterRoutes(priv)
			mods.Admin.RegisterRoutes(priv)
			mods.Events.RegisterRoutes(priv)
		})
	})

	// TODO(phase-6): 生产模式把 web/dist 挂到根路径（同源部署，去 CORS）。
	return r
}

func wellKnownHandler(cfg config.Config, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := WellKnown{
			ServerID:              cfg.ServerID,
			CanonicalURL:          cfg.PublicURL,
			APIBase:               "/api/v1",
			ProtocolVersion:       httpx.ProtocolVersion,
			MinCLIProtocolVersion: 2,
		}
		resp.Auth.DeviceLogin = true
		if resp.CanonicalURL == "" {
			scheme := "https"
			if r.TLS == nil {
				scheme = "http"
			}
			resp.CanonicalURL = scheme + "://" + r.Host
			log.Warn("ASTRAL_PUBLIC_URL not set; falling back to request Host for well-known")
		}
		httpx.WriteOK(w, r, http.StatusOK, resp)
	}
}

func readyzHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			httpx.WriteError(w, r, httpx.Unavailable("database not configured"))
			return
		}
		if err := store.Ping(r.Context(), db); err != nil {
			httpx.WriteError(w, r, httpx.Unavailable("database unreachable"))
			return
		}
		httpx.WriteOK(w, r, http.StatusOK, map[string]string{"status": "ready"})
	}
}
