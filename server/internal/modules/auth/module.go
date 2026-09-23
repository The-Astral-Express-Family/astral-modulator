package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ratelimit"
)

// Module 是 HTTP 层：路由注册 + 请求/响应编解码。业务在 Service。
type Module struct {
	Svc *Service
	// PublicURL 用于拼 verification_uri。
	PublicURL string
	// WebBaseURL 是 device flow 链接的 web 控制台基址（dev 期 web 与 API
	// 端口分离时必填）；优先级高于 PublicURL，见 deviceBaseURL。
	WebBaseURL string
	// TrustedProxy：仅当部署在可信反代之后时置 true，session 元数据的
	// 客户端 IP 才采信 X-Forwarded-For 首跳（与 ratelimit 同一信任规则，
	// env ASTRAL_TRUSTED_PROXY）。
	TrustedProxy bool
}

// RegisterPublic 挂载免鉴权的 /auth/* 端点（由 app.router 在 Authenticate 之前装配）。
// logout 携 refresh token 即可自撤，属公共端点。
func (m *Module) RegisterPublic(r chi.Router) {
	r.Post("/auth/register", m.register)
	r.Post("/auth/login", m.login)
	r.Post("/auth/token/refresh", m.refreshToken)
	r.Post("/auth/logout", m.logout)
	r.Post("/auth/device/authorizations", m.createDeviceAuthorization)
	r.Post("/auth/device/authorizations/{device_code}/token", m.exchangeDeviceToken)
}

// RegisterPrivate 挂载需要鉴权的 /auth/* 端点。路径裁决见 TODO.md D1/D2/A3。
func (m *Module) RegisterPrivate(r chi.Router) {
	r.Get("/auth/me", m.me)
	r.Patch("/auth/me", m.updateMe)
	// 设备管理（会话列表/单会话注销）—— 需 human session
	r.Get("/auth/sessions", m.listSessions)
	r.Delete("/auth/sessions/{id}", m.revokeSession)
	// 审批页 API（A3）—— 需 human session
	r.Get("/auth/device/authorizations", m.findForApproval)
	r.Post("/auth/device/authorizations/{id}/approve", m.approve)
	r.Post("/auth/device/authorizations/{id}/deny", m.deny)
}

func (m *Module) register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	// 注册成功即建会话（A5）：两分支（bootstrap/邀请兑换）同形状，
	// refresh 进 HttpOnly Cookie，响应 = Me + session（与 login 一致）。
	actor, refresh, err := m.Svc.Register(r.Context(), in, m.clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	setSessionCookie(w, r, refresh, int(m.Svc.RefreshTTL.Seconds()))
	resp := m.meBody(r.Context(), *actor)
	resp.Session = m.Svc.SessionInfoByRefresh(r.Context(), refresh)
	httpx.WriteOK(w, r, http.StatusCreated, resp)
}

func (m *Module) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	refresh, actor, err := m.Svc.Login(r.Context(), in.Email, in.Password, m.clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	setSessionCookie(w, r, refresh, int(m.Svc.RefreshTTL.Seconds()))
	resp := m.meBody(r.Context(), *actor)
	resp.Session = m.Svc.SessionInfoByRefresh(r.Context(), refresh)
	httpx.WriteOK(w, r, http.StatusOK, resp)
}

func (m *Module) refreshToken(w http.ResponseWriter, r *http.Request) {
	pair, err := m.Svc.Refresh(r.Context(), refreshTargetFrom(r), m.clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	setSessionCookie(w, r, pair.RefreshToken, int(m.Svc.RefreshTTL.Seconds()))
	httpx.WriteOK(w, r, http.StatusOK, pair)
}

// refreshTargetFrom 提取 refresh token：CLI 走 body {refresh_token}，
// Web 无 body 时回退 HttpOnly Cookie（refresh/logout 两个端点共用）。
func refreshTargetFrom(r *http.Request) string {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if in.RefreshToken != "" {
		return in.RefreshToken
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	if err := m.Svc.Logout(r.Context(), refreshTargetFrom(r)); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) me(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	var actor model.Actor
	if err := m.Svc.DB.WithContext(r.Context()).First(&actor, "id = ?", p.ActorID).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// S4-4：附 session 元数据（expires_at）。access/cookie 认证的 Principal
	// 带 SessionID；credential 认证无 session，字段省略（Me.session 非必填）。
	resp := m.meBody(r.Context(), actor)
	if p.SessionID != "" {
		resp.Session = m.Svc.SessionInfoByID(r.Context(), p.SessionID)
	}
	httpx.WriteOK(w, r, http.StatusOK, resp)
}

// updateMe 是 PATCH /auth/me：改自己的资料（display_name/bio/avatar_url）。
func (m *Module) updateMe(w http.ResponseWriter, r *http.Request) {
	var in UpdateProfileInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	p := PrincipalFrom(r.Context())
	actor, err := m.Svc.UpdateProfile(r.Context(), p.ActorID, in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, m.meBody(r.Context(), *actor))
}

// meBody 组装 Me 响应：actor DTO + human 登录邮箱 + 平台角色。邮箱查询失败
// 不致命（省略字段），身份本身已由中间件担保。
func (m *Module) meBody(ctx context.Context, actor model.Actor) MeResponse {
	resp := MeResponse{Actor: ToActorDTO(actor), PlatformRole: actor.PlatformRole}
	if email, err := m.Svc.HumanEmail(ctx, actor.ID); err == nil {
		resp.Email = email
	} else {
		m.Svc.Log.Warn("me: human email lookup failed", "actor_id", actor.ID, "err", err)
	}
	return resp
}

func (m *Module) createDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientType string `json:"client_type"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	created, err := m.Svc.CreateDeviceAuthorization(r.Context(), in.ClientType, m.deviceBaseURL(r))
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, created)
}

// deviceBaseURL 解析 device flow 验证链接的 web 侧基址（回退链见
// ResolveWebBaseURL；邀请链接共用同一实现，docs/registration.md §2.3）。
func (m *Module) deviceBaseURL(r *http.Request) string {
	return ResolveWebBaseURL(m.WebBaseURL, m.PublicURL, r)
}

// ResolveWebBaseURL 是「指向 web 前端的绝对基址」的单一实现：
// WebBaseURL（显式配置，dev 期 web 与 API 端口分离）→ PublicURL（生产同源）
// → 请求 Host（兜底）。device verification_uri 与邀请链接按 openapi
// `format: uri` 都必须是绝对 URL——客户端拿到后直接打开，不做二次拼接。
func ResolveWebBaseURL(webBaseURL, publicURL string, r *http.Request) string {
	if webBaseURL != "" {
		return webBaseURL
	}
	if publicURL != "" {
		return publicURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (m *Module) exchangeDeviceToken(w http.ResponseWriter, r *http.Request) {
	deviceCode := chi.URLParam(r, "device_code")
	pair, err := m.Svc.ExchangeDeviceToken(r.Context(), deviceCode, m.clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, pair)
}

func (m *Module) findForApproval(w http.ResponseWriter, r *http.Request) {
	// A3：审批页查询。必须 human（agent credential 不能审批人类登录）。
	if apiErr := RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	view, err := m.Svc.FindByUserCode(r.Context(), r.URL.Query().Get("user_code"))
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, view)
}

func (m *Module) approve(w http.ResponseWriter, r *http.Request) {
	m.decide(w, r, m.Svc.Approve)
}

// listSessions 是 GET /auth/sessions：设备管理页的会话列表（仅 human）。
func (m *Module) listSessions(w http.ResponseWriter, r *http.Request) {
	if apiErr := RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	views, err := m.Svc.ListSessions(r.Context(), p.ActorID, p.SessionID)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, views)
}

// revokeSession 是 DELETE /auth/sessions/{id}：注销自己的一个设备会话
//（当前浏览器会话的注销走 /auth/logout，前端负责分流）。
func (m *Module) revokeSession(w http.ResponseWriter, r *http.Request) {
	if apiErr := RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	if err := m.Svc.RevokeSession(r.Context(), p.ActorID, chi.URLParam(r, "id")); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deny(w http.ResponseWriter, r *http.Request) {
	m.decide(w, r, m.Svc.Deny)
}

func (m *Module) decide(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string) error) {
	if apiErr := RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	if err := fn(r.Context(), chi.URLParam(r, "id"), p.ActorID); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

// RequireHuman 是「必须 human session」的统一闸（agent credential 一律 403）：
// device 审批、邀请管理、全局 credential 签发/吊销同用——程序不能替人决定。
// why 是面向该端点的 403 文案。
func RequireHuman(r *http.Request, why string) *httpx.APIError {
	if p := PrincipalFrom(r.Context()); p == nil || !p.IsHuman() {
		return httpx.Forbidden(why)
	}
	return nil
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, refresh string, maxAge int) {
	if refresh == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    refresh,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// 反向代理场景：信任 X-Forwarded-Proto（部署基线即 TLS 反代）。
	// XFF 采信开关（ASTRAL_TRUSTED_PROXY）已随 S5 落地，作用于限流 key
	// 解析（internal/ratelimit）；本函数的 Proto 头信任沿用部署基线。
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (m *Module) clientIP(r *http.Request) string {
	// 单一来源：与限流共用 ratelimit.ClientIP 的信任规则（默认直连）。
	return ratelimit.ClientIP(r, m.TrustedProxy)
}
