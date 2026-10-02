// 管理员重置用户密码（协议 2.6）：授权矩阵、自我保护、口令策略、
// 目标会话全吊销与审计落地。
package admin

import (
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
)

func TestAdminResetUserPassword(t *testing.T) {
	f := newFixture(t)

	// 非 admin（含 agent credential）一律 403。
	for name, p := range map[string]*auth.Principal{"user": f.user, "agent": f.agentP} {
		rec := f.call(t, p, "POST", "/api/v1/admin/users/usr_user/password-reset",
			`{"new_password":"temppass77y"}`)
		if rec.Code != 403 {
			t.Fatalf("%s reset status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
	}

	// 自我保护：管理员不能在此重置自己（改自己的密码走 /auth/password）。
	rec := f.call(t, f.admin, "POST", "/api/v1/admin/users/"+f.admin.ActorID+"/password-reset",
		`{"new_password":"temppass77y"}`)
	if rec.Code != 400 {
		t.Fatalf("self reset status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// 口令策略不过 → 400，目标口令不变。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/password-reset",
		`{"new_password":"aaaaaaaa"}`)
	if rec.Code != 400 {
		t.Fatalf("weak reset status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// 404：目标不存在。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_nope/password-reset",
		`{"new_password":"temppass77y"}`)
	if rec.Code != 404 {
		t.Fatalf("missing target status = %d", rec.Code)
	}

	// 成功：204，目标全部会话吊销，新口令可登录，审计落一条。
	if _, _, err := f.svc.Login(t.Context(), "user@example.com", "hunter2safe", "ip", "ua"); err != nil {
		t.Fatalf("pre-login: %v", err)
	}
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/password-reset",
		`{"new_password":"temppass77y"}`)
	if rec.Code != 204 {
		t.Fatalf("admin reset status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var alive int64
	if err := f.db.Model(&model.Session{}).
		Where("actor_id = ? AND revoked_at IS NULL", "usr_user").
		Count(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if alive != 0 {
		t.Fatalf("target sessions must be revoked, %d alive", alive)
	}
	if _, _, err := f.svc.Login(t.Context(), "user@example.com", "temppass77y", "ip", "ua"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
	if _, _, err := f.svc.Login(t.Context(), "user@example.com", "hunter2safe", "ip", "ua"); err == nil {
		t.Fatal("old password must be rejected")
	}
	var audits int64
	if err := f.db.Model(&model.AuditEntry{}).
		Where("action = ? AND actor_id = ? AND target_id = ?",
			"platform.user.password_reset", f.admin.ActorID, "usr_user").
		Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("want 1 audit entry, got %d", audits)
	}
}
