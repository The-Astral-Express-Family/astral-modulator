package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// CookieName 是 web 会话 refresh token 的 HttpOnly Cookie（architecture §8.4）。
const CookieName = "astral_session"

type ctxKeyPrincipal struct{}

// PrincipalFrom 取当前请求主体；未鉴权（公共端点）返回 nil。
func PrincipalFrom(ctx context.Context) *Principal {
	if p, ok := ctx.Value(ctxKeyPrincipal{}).(*Principal); ok {
		return p
	}
	return nil
}

// WithPrincipal 把主体注入 context（Authenticate 中间件与各模块测试共用）。
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal{}, p)
}

// Authenticate 是 HTTP 中间件：解析三种凭证来源（Bearer access /
// Bearer credential / Cookie session），把 Principal 放入 context。
// 失败统一走 httpx 错误 envelope（CLI 依赖稳定 error.code）。
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var cookieRefresh string
		if c, err := r.Cookie(CookieName); err == nil {
			cookieRefresh = c.Value
		}
		p, apiErr := s.ResolvePrincipal(r.Context(), bearer, cookieRefresh)
		if apiErr != nil {
			// TODO: audit 记录认证失败（集中登记见 audit/module.go 服务器级审计条目；
			// 需要先向 Service 注入 recorder，避免中间件反向依赖装配层）。
			httpx.WriteError(w, r, apiErr)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}
