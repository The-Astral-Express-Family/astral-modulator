// 邀请码的单一 owner：码格式（生成/归一化/哈希入口）与两轨（工作区
// workspace/invitation.go、注册 admin/invitation.go + 本包 register.go）
// 重复实现的策略（失效哨兵、TTL、invite_url 拼装）。会话凭证家族
// （ata_/atr_/adc_/prt_/astral_）见 tokens.go——两者同用 HashToken 基元，
// 但防御对象与生命周期不同，不混居。
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

// inviteCodeAlphabet 是 Crockford base32（去 I/L/O/U）。恰好 32 字符，
// byte%32 无取样偏差；20 字符 = 100 bit 熵。
const inviteCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewInviteCode 生成一次性邀请码，分组展示形 XXXXX-XXXXX-XXXXX-XXXXX。
// 与 user_code（人短时手输）不同：邀请码要在邮箱/聊天里存活数天，防御对象
// 是离线爆破，因此熵高两个量级（docs/registration.md §2.2）。
func NewInviteCode() (string, error) {
	body, err := randomFromAlphabet(inviteCodeAlphabet, 20)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{body[:5], body[5:10], body[10:15], body[15:]}, "-"), nil
}

// NormalizeInviteCode 是兑换时的码归一化（比对前唯一入口）：去分隔符、大写。
// 库中 code_hash 一律基于归一化形式计算。
func NormalizeInviteCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(code, "-", ""))
}

// InviteCodeHash 是「明文码 → 库中 code_hash」的唯一拼写：签发侧落库与
// 兑换侧查库都必须经此入口，保证归一化规则改动只有一处生效。
func InviteCodeHash(code string) string {
	return HashToken(NormalizeInviteCode(code))
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
