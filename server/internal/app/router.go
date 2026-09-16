// Package app 装配 HTTP 路由：公共中间件、发现端点、健康检查、
// 鉴权边界与各业务模块。新模块一律通过模块自身的 Register* 挂载到 /api/v1，
// 不得自起 http.Server 或绕过中间件。
package app

import (
	"log/slog"
	"net/http"
	"strings"
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
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ratelimit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
	"github.com/The-Astral-Express-Family/astral-modulator/server/webdist"
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
		// 版本协商（R2/NFR-004）：带 X-Astral-Client-Version 头且低于
		// min_cli_protocol_version 的请求直接 400；头缺失放行（浏览器/测试）。
		api.Use(httpx.ClientVersionMiddleware)

		// S5 限流（docs/protocol.md §6）：进程内 token bucket，两段式挂载
		//（语义详表见 internal/ratelimit 包注释；auth 公共/私有路由由
		// RegisterPublic/RegisterPrivate 整体注册，按路由分组挂会拆散模块
		// 装配，故路径分类收在中间件内做）：
		//   Public（此处，鉴权前）——敏感/轮询桶按客户端 IP；logout 与
		//   capabilities 按 IP 记入通用桶；受保护路径放行不记账。
		//   Private（Authenticate 之后，见下方 priv 组）——设备审批三端点
		//   入敏感桶（IP key）、events 入 SSE 桶、其余入通用桶（actor key）。
		// 两段互斥分工，单个请求只进一个桶，不双计。
		// 零值 cfg.RateLimit = 四桶全禁用（既有集成测试直接构造 Config{}
		// 即免限流自伤；生产默认值由 config.Load 填充，env 可覆盖）。
		rl := ratelimit.New(ratelimit.Config{
			SensitivePerMin: cfg.RateLimit.SensitivePerMin,
			PollPerMin:      cfg.RateLimit.PollPerMin,
			APIPerMin:       cfg.RateLimit.APIPerMin,
			SSEPerMin:       cfg.RateLimit.SSEPerMin,
			TrustedProxy:    cfg.TrustedProxy,
		}, actorLimitKey)
		api.Use(rl.Public)
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

		// 受保护 API。认证失败的审计在 auth.Service 各失败分支落库（S4-3，
		// action=auth.bearer 等；成功路径不写——R7，sessions 表自身即事实）。
		api.Group(func(priv chi.Router) {
			priv.Use(mods.Auth.Svc.Authenticate)
			// 限流 Private 段：Authenticate 之后 key 才能取到 actor_id
			//（通用/SSE 桶按 actor 记账；与 Public 段互斥分工，不双计）。
			// 挂在幂等之前：被 429 的请求不应消耗幂等键的互斥窗口。
			priv.Use(rl.Private)
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

	// 生产同源托管（S6-1）：`make web-dist` 把 web/dist 构建产物拷入
	// server/webdist/dist（go:embed 编译期嵌入，见 webdist 包）后，根级
	// 未匹配的 GET 交给静态文件服务 + SPA fallback（未命中回 index.html，
	// 供前端路由刷新/深链）。挂在根 NotFound 兜底而非注册 /* 通配路由：
	// 上方 /api/v1、/.well-known/astral、/healthz、/readyz 等具体路由必然
	// 先行命中（/api/v1 子树内未知路径由其自身 404 envelope 兜底），且
	// 契约门（openapi_contract_test 的 chi.Walk）只见具体路由，静态托管
	// 对公网契约不可见。保留前缀与非 GET 维持 404——它们不属于前端。
	// dist 仅 .gitkeep 占位（未执行 web-dist）时不挂载，行为与本特性之前一致。
	if webdist.Available() {
		spa := webdist.Handler()
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet && !reservedPath(req.URL.Path) {
				spa.ServeHTTP(w, req)
				return
			}
			http.NotFound(w, req)
		})
	}

	return r
}

// reservedPath 判定服务保留前缀：/api、/.well-known、/healthz、/readyz。
// 具体端点已在 NewRouter 注册、先于 NotFound 命中；此处兜底其未知子路径
// （如 /api/v2、/.well-known/other），不把 SPA 壳喂给接口探测。
func reservedPath(p string) bool {
	return p == "/healthz" || p == "/readyz" || p == "/api" || p == "/.well-known" ||
		strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/.well-known/")
}

// actorLimitKey 提取限流记账用的主体标识（S5：认证后 = actor_id）。
// 挂在 priv 组（Authenticate 之后）调用，理论上必有 principal；
// 返回空串时 ratelimit 回退客户端 IP（防御路径）。
func actorLimitKey(r *http.Request) string {
	if p := auth.PrincipalFrom(r.Context()); p != nil {
		return p.ActorID
	}
	return ""
}

func wellKnownHandler(cfg config.Config, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := WellKnown{
			ServerID:              cfg.ServerID,
			CanonicalURL:          cfg.PublicURL,
			APIBase:               "/api/v1",
			ProtocolVersion:       httpx.ProtocolVersion,
			MinCLIProtocolVersion: httpx.MinCLIProtocolVersion,
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
