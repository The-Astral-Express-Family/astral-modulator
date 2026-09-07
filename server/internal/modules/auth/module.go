package auth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// Module 依赖注入容器。Phase 1 落地时补 DB、token service、audit。
type Module struct {
	Log *slog.Logger
	// TODO(phase-1): DB *gorm.DB、Issuer *TokenService、Auditor。
}

// RegisterRoutes 挂载 auth 路由。
//
// 契约注意：docs/architecture.md §8.2 与 docs/protocol.md §8 的路径不一致，
// 已统一为 architecture 版本（与 astral-cli 文档一致）：
//
//	POST /api/v1/auth/device/authorizations
//	POST /api/v1/auth/device/authorizations/{device_code}/token
//	POST /api/v1/auth/token/refresh
//	POST /api/v1/auth/logout
//	GET  /api/v1/auth/me            ← protocol.md 写作 /auth/session，已统一为 /auth/me
//
// 差异清单记录在 TODO.md「文档分歧裁决」；openapi.yaml 是唯一事实来源。
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/device/authorizations", m.createDeviceAuthorization)
		r.Post("/device/authorizations/{device_code}/token", m.exchangeDeviceToken)
		r.Post("/token/refresh", m.refreshToken)
		r.Post("/logout", m.logout)
		r.Get("/me", m.me)
	})
}

func (m *Module) createDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-1): Device Flow 第一步。
	//  - 生成 device_code（≥128-bit 随机，只存 hash）与 user_code；
	//  - 返回 verification_uri / verification_uri_complete / expires_in=600 / interval=3；
	//  - user_code 防枚举限流；写入 audit（outcome=allowed/denied）。
	//  依据：architecture §8.2、protocol §8、security.md。
	httpx.NotImplemented(w, r, "auth.device.authorizations.create", "phase-1", "docs/architecture.md §8.2")
}

func (m *Module) exchangeDeviceToken(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-1): 用 device_code 轮询兑换 access+refresh。
	//  - pending → 返回 428/慢轮询约定（openapi 已定义错误形状）；
	//  - approved → 单次兑换，签发 opaque token（access 5-15min，refresh 30d 轮换）；
	//  - 过期/已兑换 → 明确错误码，绝不二次发放。
	httpx.NotImplemented(w, r, "auth.device.token.exchange", "phase-1", "docs/architecture.md §8.2")
}

func (m *Module) refreshToken(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-1): refresh 轮换 + 重放检测。
	//  - 验证 refresh hash → 发新 access+refresh（同事务作废旧值）；
	//  - 检测旧值重放 → 撤销整个 session family + audit + 断开相关 SSE；
	//  - POST /logout 与 revoke 复用同一撤销路径。
	httpx.NotImplemented(w, r, "auth.token.refresh", "phase-1", "docs/architecture.md §8.3")
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-1): 撤销当前 session；Web 场景同时清除 HttpOnly Cookie。
	httpx.NotImplemented(w, r, "auth.logout", "phase-1", "docs/architecture.md §8.3")
}

func (m *Module) me(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-1): 返回当前 actor（id/kind/display_name）与 client session 元数据。
	// CLI login 成功后立即调用本端点自检（astral-cli ARCHITECTURE §6.2）。
	httpx.NotImplemented(w, r, "auth.me", "phase-1", "docs/architecture.md §8.2")
}

// Authenticate 是所有受保护路由的鉴权中间件骨架。
// TODO(phase-1): 实装后接入 app.router 的 RequireAuth 组：
//   - Authorization: Bearer <token> → credential/access-token 校验（只比对 hash）；
//   - Web Cookie session → 同一 Human actor 的另一 client session；
//   - 结果写入 context：actor id、kind、生效 scope 集合；
//   - 失败：AUTH_REQUIRED / TOKEN_EXPIRED / TOKEN_REVOKED / INSUFFICIENT_SCOPE，
//     并按 security.md 记录授权失败 audit。
func (m *Module) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = context.Background() // 占位使用 context；实装时把 actor 塞进 request context
		// 脚手架阶段：放行并在日志标记 anonymous，让 GUI/CLI 先能打桩联调。
		m.Log.Warn("auth bypassed (scaffold)", "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// RequireScopes 返回校验 scope 的中间件（骨架）。
// TODO(phase-1): 从 context 取 scope 集合做交集判断，缺失时 INSUFFICIENT_SCOPE(403)。
func RequireScopes(needed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = needed
			next.ServeHTTP(w, r)
		})
	}
}
