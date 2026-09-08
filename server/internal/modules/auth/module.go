package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// Module 是 HTTP 层：路由注册 + 请求/响应编解码。业务在 Service。
type Module struct {
	Svc *Service
	// PublicURL 用于拼 verification_uri。
	PublicURL string
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
	actor, err := m.Svc.Register(r.Context(), in)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, MeResponse{Actor: *actor})
}

func (m *Module) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	refresh, actor, err := m.Svc.Login(r.Context(), in.Email, in.Password, clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	setSessionCookie(w, r, refresh, int(m.Svc.RefreshTTL.Seconds()))
	httpx.WriteOK(w, r, http.StatusOK, MeResponse{
		Actor:   *actor,
		Session: &SessionInfo{ClientType: "web"},
	})
}

func (m *Module) refreshToken(w http.ResponseWriter, r *http.Request) {
	// CLI：body {refresh_token}；Web：HttpOnly Cookie。
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	token := in.RefreshToken
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	pair, err := m.Svc.Refresh(r.Context(), token, clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	setSessionCookie(w, r, pair.RefreshToken, int(m.Svc.RefreshTTL.Seconds()))
	httpx.WriteOK(w, r, http.StatusOK, pair)
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	token := in.RefreshToken
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	if err := m.Svc.Logout(r.Context(), token); err != nil {
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
	// TODO(phase-2): 附 session 过期时间（Principal 已带 SessionID）。
	httpx.WriteOK(w, r, http.StatusOK, MeResponse{Actor: actor})
}

func (m *Module) createDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientType string `json:"client_type"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	created, err := m.Svc.CreateDeviceAuthorization(r.Context(), in.ClientType, m.PublicURL)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, created)
}

func (m *Module) exchangeDeviceToken(w http.ResponseWriter, r *http.Request) {
	deviceCode := chi.URLParam(r, "device_code")
	pair, err := m.Svc.ExchangeDeviceToken(r.Context(), deviceCode, clientIP(r), r.UserAgent())
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, pair)
}

func (m *Module) findForApproval(w http.ResponseWriter, r *http.Request) {
	// A3：审批页查询。必须 human（agent credential 不能审批人类登录）。
	p := PrincipalFrom(r.Context())
	if !p.IsHuman() {
		httpx.WriteError(w, r, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "human session required"})
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

func (m *Module) deny(w http.ResponseWriter, r *http.Request) {
	m.decide(w, r, m.Svc.Deny)
}

func (m *Module) decide(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string) error) {
	p := PrincipalFrom(r.Context())
	if !p.IsHuman() {
		httpx.WriteError(w, r, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "human session required"})
		return
	}
	if err := fn(r.Context(), chi.URLParam(r, "id"), p.ActorID); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

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
	// TODO(phase-6): 只信任可配置的可信代理列表。
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	// MVP：直连地址 + 常见代理头；多级代理信任链留待 phase-6。
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
