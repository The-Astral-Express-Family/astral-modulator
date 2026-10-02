// 忘记密码：email 一次性重置 token（00023 / docs/security.md 密码重置节）。
//
// 端点（auth.Module RegisterPublic，均入 S5 敏感限流桶）：
//
//	POST /auth/password-reset           {email}        → 恒 204（防枚举）
//	POST /auth/password-reset/confirm   {token,new_password} → 204 / 400
//
// 语义要点：
//   - 请求端点无论邮箱是否存在都 204——存在性只影响「是否真的发了邮件」；
//   - 单活跃 token：同一账号新请求立即作废旧 token（补写 used_at），邮件里
//     永远只有最新一封可兑；
//   - 明文 token 只随邮件链接出服务器（log transport 下在服务器日志里）；
//     库中 sha256，比对常数时间（HashEqual）；
//   - 兑换 = 条件更新抢状态（used_at IS NULL AND expires_at > now），并发
//     双用只有一个赢家；新口令策略校验先于消耗——弱口令报 VALIDATION_FAILED
//     不烧 token；
//   - 重置成功吊销该账号全部会话（RevokeActorSessions，含 SSE 断流）——
//     重置的威胁模型即「当前凭证可能已泄露」。
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/mail"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
)

// PasswordResetTTL 是重置 token 的有效期（30 分钟：足够收邮件点链接，
// 又限制明文 token 在邮箱/日志中的暴露窗口）。
const PasswordResetTTL = 30 * time.Minute

// errPasswordResetInvalid 是 confirm 的统一拒绝：查无/过期/已用/停用账号
// 同码同文案（与 INVITE_INVALID 同哲学：持有者对失效原因无合法需求，
// 也不给探测者区分信号）。
var errPasswordResetInvalid = &httpx.APIError{
	Status:  http.StatusBadRequest,
	Code:    httpx.CodePasswordResetInvalid,
	Message: "password reset token is invalid or expired",
}

// RequestPasswordReset 受理忘记密码请求。返回 nil 时 HTTP 层写 204——
// 无论邮箱是否存在（防枚举）；不存在/停用账号是静默 no-op。
// webBaseURL 是 web 控制台基址（链接 {base}/reset-password?token=...，
// 回退链同 device flow 的 ResolveWebBaseURL）。
func (s *Service) RequestPasswordReset(ctx context.Context, email, ip, webBaseURL string) error {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	var ok bool
	if email, ok = NormalizeEmail(email); !ok {
		return httpx.Invalid("invalid email")
	}
	var ha model.HumanAuth
	err := s.DB.WithContext(ctx).Where("email = ?", email).First(&ha).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 恒 204：不落审计（审计可见者本就另有管理面，这里不留邮箱探测面）。
		return nil
	}
	if err != nil {
		return err
	}
	// 停用账号静默跳过：登录本就被拒，重置无意义；对外仍 204。
	var actor model.Actor
	if err := s.DB.WithContext(ctx).First(&actor, "id = ?", ha.ActorID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // human_auth 残行（actor 已删）：与不存在同待遇
		}
		return err
	}
	if actor.DisabledAt != nil {
		return nil
	}

	token, err := NewPasswordResetToken()
	if err != nil {
		return err
	}
	now := time.Now()
	row := model.PasswordResetToken{
		ID:          ids.New(ids.PasswordReset),
		HumanAuthID: ha.ActorID,
		TokenHash:   HashToken(token),
		ExpiresAt:   now.Add(PasswordResetTTL),
		CreatedAt:   now,
		RequestIP:   ip,
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 单活跃：同账号在途 token 立即作废（含其邮件链接）。
		if err := tx.Model(&model.PasswordResetToken{}).
			Where("human_auth_id = ? AND used_at IS NULL", ha.ActorID).
			Update("used_at", now).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: ha.ActorID,
			Action:  "auth.password_reset_request", Outcome: "allowed",
			TargetType: "actor", TargetID: ha.ActorID,
			Details: map[string]any{"expires_at": row.ExpiresAt.UTC().Format(time.RFC3339)},
		})
	})
	if err != nil {
		return err
	}

	// 投递失败不影响 204：token 行已落库，log transport 下链接仍在服务器
	// 日志里（管理员可捞）；SMTP 模式失败仅记错误日志，用户可重试（会再
	// 作废本 token，无放大风险）。
	link := strings.TrimRight(webBaseURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
	msg := resetEmail(actor.DisplayName, email, link)
	if err := mail.OrLog(s.Mailer, s.Log).Send(ctx, msg); err != nil {
		s.Log.Error("password reset mail delivery failed", "actor_id", ha.ActorID, "err", err)
	}
	return nil
}

// ConfirmPasswordReset 兑换一次性 token：验 token → 烧 token → 换口令哈希 →
// 吊销全部会话。新口令策略校验先于 token 消耗（弱口令 400 VALIDATION_FAILED，
// token 不烧，用户可同链接重试）。
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return errPasswordResetInvalid
	}
	hash, apiErr := hashPasswordOrInvalid(newPassword)
	if apiErr != nil {
		return apiErr
	}

	var row model.PasswordResetToken
	err := s.DB.WithContext(ctx).Where("token_hash = ?", HashToken(token)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errPasswordResetInvalid
	}
	if err != nil {
		return err
	}
	var actor model.Actor
	if err := s.DB.WithContext(ctx).First(&actor, "id = ?", row.HumanAuthID).Error; err != nil {
		return errPasswordResetInvalid // 残行防御：actor 已删
	}
	if actor.DisabledAt != nil {
		return errPasswordResetInvalid // 停用账号不暴露区分信号
	}

	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 条件更新抢状态：过期/已用/并发双用一律 0 行 → 统一拒绝。
		res := tx.Model(&model.PasswordResetToken{}).
			Where("id = ? AND used_at IS NULL AND expires_at > ?", row.ID, time.Now()).
			Update("used_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errPasswordResetInvalid
		}
		if err := tx.Model(&model.HumanAuth{}).
			Where("actor_id = ?", row.HumanAuthID).
			Update("password_hash", hash).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: row.HumanAuthID,
			Action:  "auth.password_reset", Outcome: "allowed",
			TargetType: "actor", TargetID: row.HumanAuthID,
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordResetInvalid) {
			return errPasswordResetInvalid
		}
		return err
	}
	// 会话吊销在事务外：即使吊销写失败（库故障），口令已换——旧口令登录
	// 已不可能，旧会话最迟 30d 自然过期。
	if n, err := s.RevokeActorSessions(ctx, row.HumanAuthID); err != nil {
		s.Log.Error("revoke sessions after password reset failed", "actor_id", row.HumanAuthID, "err", err)
	} else {
		s.Log.Info("password reset", "actor_id", row.HumanAuthID, "revoked_sessions", n)
	}
	return nil
}

// resetEmail 组装重置邮件（中文正文；HTML 与文本同内容）。
func resetEmail(displayName, to, link string) mail.Message {
	subject := "重置你的 Astral 账号密码"
	text := fmt.Sprintf(`%s：

你（或他人）请求重置 Astral 账号密码。打开下面的链接设置新密码，30 分钟内有效（仅可使用一次）：

%s

如果这不是你本人的操作，忽略本邮件即可——现有密码不会改变。
重置成功后，该账号所有已登录会话将被注销。`, displayName, link)
	html := fmt.Sprintf(`<p>%s：</p>
<p>你（或他人）请求重置 Astral 账号密码。打开下面的链接设置新密码，30 分钟内有效（仅可使用一次）：</p>
<p><a href="%s">重置密码</a></p>
<p style="font-family:monospace;font-size:12px;word-break:break-all">%s</p>
<p>如果这不是你本人的操作，忽略本邮件即可——现有密码不会改变。重置成功后，该账号所有已登录会话将被注销。</p>`,
		displayName, link, link)
	return mail.Message{To: to, Subject: subject, Text: text, HTML: html}
}
