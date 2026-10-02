// 两轨邀请（工作区 workspace/invitation.go、注册 admin/invitation.go）的
// 共享件：失效哨兵、TTL 规则、invite_url 拼装。码格式与归一化见 tokens.go
// （NewInviteCode/NormalizeInviteCode）；本文件只收各端点重复实现的策略。
package auth

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// ErrInviteInvalid 是邀请码失效的统一错误：不存在/已兑换/已撤销/已过期
// 同码同文案，不给区分（防探测，docs/registration.md §3）。两轨兑换端点
// （注册码在 Register、工作区码在 /invitations/redeem）共用此哨兵。
var ErrInviteInvalid = &httpx.APIError{
	Status:  http.StatusBadRequest,
	Code:    httpx.CodeInviteInvalid,
	Message: "invite code is invalid or expired",
}

// 邀请 TTL（两轨同规）：默认 7d，签发时 expires_in 秒可指定，上限 30d。
const (
	DefaultInviteTTL = 7 * 24 * time.Hour
	MaxInviteTTL     = 30 * 24 * time.Hour
)

// ParseInviteTTL 解析签发请求的 expires_in（秒）：nil → 默认 TTL；非法
// （≤0 或超上限）→ 统一 400 VALIDATION_FAILED（文案两轨一致）。
func ParseInviteTTL(expiresIn *int64) (time.Duration, *httpx.APIError) {
	if expiresIn == nil {
		return DefaultInviteTTL, nil
	}
	if *expiresIn <= 0 || *expiresIn > int64(MaxInviteTTL/time.Second) {
		return 0, httpx.Invalid("expires_in must be between 1 and 2592000 seconds")
	}
	return time.Duration(*expiresIn) * time.Second, nil
}

// InviteLink 在 ResolveWebBaseURL 基址上拼邀请深链：pathPrefix 形如
// "/join?ws="（工作区码，面向已注册用户）或 "/register?code="（注册码），
// value 做 query 转义。两类邀请的 invite_url 共用此拼装点，基址回退链
// 与 device verification_uri 同源。
func InviteLink(webBaseURL, publicURL string, r *http.Request, pathPrefix, value string) string {
	base := ResolveWebBaseURL(webBaseURL, publicURL, r)
	return strings.TrimRight(base, "/") + pathPrefix + url.QueryEscape(value)
}
