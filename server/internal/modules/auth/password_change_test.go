// 口令管理（协议 2.6）服务层测试：自助改密验当前口令、保留当前会话吊销
// 其余、策略先行；管理员重置直接换哈希并吊销目标全部会话。
package auth

import (
	"context"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// loginSessions 以 n 个会话登录指定账号（createSession 返回 refresh），
// 返回 actor 与各会话的 session ID。
func loginSessions(t *testing.T, s *Service, actorID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for range n {
		_, _, sess, err := s.createSession(context.Background(), actorID, "web", "ip", "ua")
		if err != nil {
			t.Fatalf("createSession: %v", err)
		}
		ids = append(ids, sess.ID)
	}
	return ids
}

func humanAuthHash(t *testing.T, s *Service, actorID string) string {
	t.Helper()
	var ha model.HumanAuth
	if err := s.DB.First(&ha, "actor_id = ?", actorID).Error; err != nil {
		t.Fatal(err)
	}
	return ha.PasswordHash
}

func TestChangePasswordVerifiesCurrentPassword(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := registerHuman(t, s, "u@example.com", "hunter2safe")
	loginSessions(t, s, actor.ID, 1)
	before := humanAuthHash(t, s, actor.ID)

	err := s.ChangePassword(ctx, actor.ID, "", "wrong-old-pass1", "newpass999x")
	if apiCode(t, err) != httpx.CodePasswordMismatch {
		t.Fatalf("want PASSWORD_MISMATCH, got %v", err)
	}
	if got := humanAuthHash(t, s, actor.ID); got != before {
		t.Fatal("hash must not change on wrong current password")
	}
}

func TestChangePasswordPolicyRejected(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := registerHuman(t, s, "u@example.com", "hunter2safe")
	before := humanAuthHash(t, s, actor.ID)

	// 弱口令在当前口令验证通过后仍被策略闸拒绝（VALIDATION_FAILED）；
	// "aaaaaaaa" 无数字，过不了策略。
	if err := s.ChangePassword(ctx, actor.ID, "", "hunter2safe", "aaaaaaaa"); apiCode(t, err) != httpx.CodeValidationFailed {
		t.Fatalf("want VALIDATION_FAILED, got %v", err)
	}
	if got := humanAuthHash(t, s, actor.ID); got != before {
		t.Fatal("hash must not change on policy failure")
	}
}

func TestChangePasswordKeepsCurrentSession(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := registerHuman(t, s, "u@example.com", "hunter2safe")
	sessions := loginSessions(t, s, actor.ID, 3)
	keep := sessions[1] // 模拟发起改密请求的那个会话

	if err := s.ChangePassword(ctx, actor.ID, keep, "hunter2safe", "newpass999x"); err != nil {
		t.Fatalf("change: %v", err)
	}
	// 其余会话全部吊销，当前会话存活。
	var alive []model.Session
	if err := s.DB.Where("actor_id = ? AND revoked_at IS NULL", actor.ID).Find(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if len(alive) != 1 || alive[0].ID != keep {
		t.Fatalf("want only %s alive, got %+v", keep, alive)
	}
	// 旧口令登录失败、新口令成功。
	if _, _, err := s.Login(ctx, "u@example.com", "hunter2safe", "ip", "ua"); err == nil {
		t.Fatal("old password must be rejected")
	}
	if _, _, err := s.Login(ctx, "u@example.com", "newpass999x", "ip", "ua"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestSetPasswordByAdminReplacesAndRevokes(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := registerHuman(t, s, "u@example.com", "hunter2safe")
	loginSessions(t, s, actor.ID, 2)

	// 策略闸先于任何状态变更。
	if err := s.SetPasswordByAdmin(ctx, "usr_admin", actor.ID, "short"); apiCode(t, err) != httpx.CodeValidationFailed {
		t.Fatalf("want VALIDATION_FAILED, got %v", err)
	}

	if err := s.SetPasswordByAdmin(ctx, "usr_admin", actor.ID, "temppass77y"); err != nil {
		t.Fatalf("admin reset: %v", err)
	}
	var alive int64
	if err := s.DB.Model(&model.Session{}).
		Where("actor_id = ? AND revoked_at IS NULL", actor.ID).
		Count(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if alive != 0 {
		t.Fatalf("all target sessions must be revoked, %d alive", alive)
	}
	if _, _, err := s.Login(ctx, "u@example.com", "temppass77y", "ip", "ua"); err != nil {
		t.Fatalf("login with admin-set password: %v", err)
	}
	if _, _, err := s.Login(ctx, "u@example.com", "hunter2safe", "ip", "ua"); err == nil {
		t.Fatal("old password must be rejected")
	}
}
