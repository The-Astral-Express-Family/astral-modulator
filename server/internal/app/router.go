// Package app 装配 HTTP 路由：公共中间件、发现端点、能力端点、健康检查与各业务模块。
// 新模块一律通过 RegisterRoutes 挂载到 /api/v1，不得自起 http.Server 或绕过中间件。
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
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

// Capabilities 是 GET /api/v1/meta/capabilities 的响应（protocol §17）。
// 客户端依据 features 判断功能，不得猜测 server 实现版本。
type Capabilities struct {
	ProtocolVersion   int      `json:"protocol_version"`
	MinimumCliVersion string   `json:"minimum_cli_version"`
	Features          []string `json:"features"`
}

type Server struct {
	cfg config.Config
	log *slog.Logger
	db  DB
	hub *event.Hub
}

// DB 是 store 的最小接口（便于无数据库模式与测试替换）。
type DB interface {
	Ping() error
}

// Router 构建完整 http.Handler。
func NewRouter(cfg config.Config, log *slog.Logger, db DB, hub *event.Hub) http.Handler {
	s := &Server{cfg: cfg, log: log, db: db, hub: hub}

	r := chi.NewRouter()
	r.Use(httpx.Recover)
	r.Use(httpx.Logger(log))
	r.Use(httpx.RequestIDMiddleware)
	if len(cfg.DevCORSOrigins) > 0 {
		r.Use(httpx.CORS(cfg.DevCORSOrigins))
	}

	// 发现端点（无认证）。api_base 固定 /api/v1；若未来变更属 breaking change。
	r.Get("/.well-known/astral", s.wellKnown)

	// 运维端点（无认证；不要在此泄露内部信息）。
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	// 公网 API v1。
	r.Route("/api/v1", func(api chi.Router) {
		// 未知 /api/v1 路径也返回统一 envelope（CLI 依赖 error.code 而非默认 404 页）。
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, &httpx.APIError{
				Status:  http.StatusNotFound,
				Code:    httpx.CodeInternalError,
				Message: "no such endpoint under /api/v1",
			})
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, &httpx.APIError{
				Status:  http.StatusMethodNotAllowed,
				Code:    httpx.CodeValidationFailed,
				Message: "method not allowed",
			})
		})

		api.Get("/meta/capabilities", s.capabilities)

		// 业务模块。TODO(phase-1): 实装鉴权后，把写操作组套 authMod.Authenticate
		// 与 auth.RequireScopes(...)；当前桩阶段放行 + 日志警告（见 auth module）。
		// TODO(phase-1): 各模块补 DB/audit 依赖注入（当前为无状态桩）。
		(&auth.Module{Log: log}).RegisterRoutes(api)
		new(workspace.Module).RegisterRoutes(api)
		new(task.Module).RegisterRoutes(api)
		new(tag.Module).RegisterRoutes(api)
		new(memory.Module).RegisterRoutes(api)
		new(document.Module).RegisterRoutes(api)
		new(message.Module).RegisterRoutes(api)
		new(presence.Module).RegisterRoutes(api)
		new(audit.Module).RegisterRoutes(api)
		(&event.SSEHandler{Hub: hub}).RegisterRoutes(api)
	})

	// TODO(phase-6): 生产模式下把 web/dist 作为静态资源挂到根路径（同源部署，
	// 消除 CORS）；开发期 Web 走 Vite 5173 + 代理。

	return r
}

func (s *Server) wellKnown(w http.ResponseWriter, r *http.Request) {
	resp := WellKnown{
		ServerID:              s.cfg.ServerID,
		CanonicalURL:          s.cfg.PublicURL,
		APIBase:               "/api/v1",
		ProtocolVersion:       httpx.ProtocolVersion,
		MinCLIProtocolVersion: 1,
	}
	resp.Auth.DeviceLogin = true
	if resp.CanonicalURL == "" {
		// 未配置 ASTRAL_PUBLIC_URL：回退请求 Host。CLI 只把它当展示值；
		// server_id 才是绑定主键（astral-cli §5）。
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		resp.CanonicalURL = scheme + "://" + r.Host
		s.log.Warn("ASTRAL_PUBLIC_URL not set; falling back to request Host for well-known")
	}
	httpx.WriteOK(w, r, http.StatusOK, resp)
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	// features 按实现进度逐步开放；未列出的功能 CLI 必须视为不可用。
	// TODO(phase-3): 首个真实 feature（task_lease）上线时加入并补契约测试。
	httpx.WriteOK(w, r, http.StatusOK, Capabilities{
		ProtocolVersion:   httpx.ProtocolVersion,
		MinimumCliVersion: "0.1.0",
		Features:          []string{},
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteOK(w, r, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		httpx.WriteError(w, r, &httpx.APIError{
			Status:  http.StatusServiceUnavailable,
			Code:    httpx.CodeInternalError,
			Message: "database not configured",
		})
		return
	}
	if err := s.db.Ping(); err != nil {
		httpx.WriteError(w, r, &httpx.APIError{
			Status:  http.StatusServiceUnavailable,
			Code:    httpx.CodeInternalError,
			Message: "database unreachable",
		})
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

// PingDB 适配 store 的数据库句柄到 DB 接口（无数据库开发模式传 nil）。
type PingDB struct{ Pinger func() error }

func (p PingDB) Ping() error { return p.Pinger() }
