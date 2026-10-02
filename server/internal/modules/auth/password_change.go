// 口令管理（协议 2.6，TODO.md §9 2026-10-02）：自助改密 + 管理员重置。
// 与忘记密码（reset.go）互补——那条链路的威胁模型是「凭证可能已泄露」，
// 走邮件所有权验证；本文件两条路径的主体身份已由会话/管理员授权担保：
//
//	ChangePassword    自助：验当前口令 → 换哈希 → 吊销除当前会话外全部会话
//	SetPasswordByAdmin 管理员：直接设新口令 → 吊销目标全部会话（等价停用链
//	                   的会话处理；目标无需在场，走带外告知新口令）
//
// 两条路径都是「换口令」这个领域事实：领域表变更 + audit 同事务，会话
// 吊销在事务外（与 reset.go 同理由：即使吊销写失败，旧口令登录已不可能）。
package auth

import (
	"context"
	"errors"
	"net/http"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
)

// errPasswordMismatch 是自助改密当前口令不符的统一拒绝（400）。刻意不用
// 401：web 拦截器把认证端点的 401 解释为会话失效并走登出，口令打错就
// 被登出是事故；400 才是表单可处理的形状（httpx/errors.go 同注）。
var errPasswordMismatch = &httpx.APIError{
	Status:  http.StatusBadRequest,
	Code:    httpx.CodePasswordMismatch,
	Message: "current password is incorrect",
}

// ChangePassword 自助改密：验证当前口令（防会话劫持者直接换锁）→ 新口令
// 策略校验 → 换哈希 + 审计（同事务）→ 吊销除当前会话外的全部会话。
// currentSessionID 为空（理论上不可能：human 只经 access/cookie 认证到达
// 此处）时退化为吊销全部——保守方向不出错。
func (s *Service) ChangePassword(ctx context.Context, actorID, currentSessionID, currentPassword, newPassword string) error {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	var ha model.HumanAuth
	err := s.DB.WithContext(ctx).First(&ha, "actor_id = ?", actorID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// human actor 必有 human_auth 行（注册同事务创建）；残行防御。
		return httpx.NotFound("human auth not found")
	}
	if err != nil {
		return err
	}
	if !CheckPassword(currentPassword, ha.PasswordHash) {
		return errPasswordMismatch
	}
	hash, apiErr := hashPasswordOrInvalid(newPassword)
	if apiErr != nil {
		return apiErr
	}

	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.HumanAuth{}).
			Where("actor_id = ?", actorID).
			Update("password_hash", hash).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: actorID,
			Action:  "auth.password_change", Outcome: "allowed",
			TargetType: "actor", TargetID: actorID,
		})
	})
	if err != nil {
		return err
	}
	if n, err := s.RevokeActorSessionsExcept(ctx, actorID, currentSessionID); err != nil {
		s.Log.Error("revoke sessions after password change failed", "actor_id", actorID, "err", err)
	} else {
		s.Log.Info("password changed", "actor_id", actorID, "revoked_sessions", n)
	}
	return nil
}

// SetPasswordByAdmin 是管理员重置（POST /admin/users/{actor_id}/password-reset）
// 的领域面：不做授权判断（调用方 admin 模块已过 RequireGlobal 与 self-gate），
// 只负责「设新口令 + 以管理员身份落审计」。停用账号允许重置（管理员先重置
// 后恢复的恢复路径）；human_auth 残行（actor 已删）按 0 行更新拒绝。
func (s *Service) SetPasswordByAdmin(ctx context.Context, adminID, targetActorID, newPassword string) error {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return dbErr
	}
	hash, apiErr := hashPasswordOrInvalid(newPassword)
	if apiErr != nil {
		return apiErr
	}

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.HumanAuth{}).
			Where("actor_id = ?", targetActorID).
			Update("password_hash", hash)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return httpx.NotFound("human auth not found")
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: adminID,
			Action:  "platform.user.password_reset", Outcome: "allowed",
			TargetType: "actor", TargetID: targetActorID,
		})
	})
	if err != nil {
		return err
	}
	if n, err := s.RevokeActorSessions(ctx, targetActorID); err != nil {
		s.Log.Error("revoke sessions after admin password reset failed", "actor_id", targetActorID, "err", err)
	} else {
		s.Log.Info("password reset by admin", "actor_id", targetActorID, "revoked_sessions", n)
	}
	return nil
}
