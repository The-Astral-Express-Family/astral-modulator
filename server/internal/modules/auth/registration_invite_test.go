// 注册邀请兑换（00018 落地，00019 更名；ADR-0009）：registerWithRegistrationCode
// 只查 registration_invitations——建号为普通 user，不入任何 workspace。
// 契约门（工作区码在注册端点被拒）与边界语义见 invite_test.go。
package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// seedRegistrationInvite 直插一张平台邀请，返回明文码。
func seedRegistrationInvite(t *testing.T, s *Service, mutate func(*model.RegistrationInvitation)) string {
	t.Helper()
	code, err := NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	inv := model.RegistrationInvitation{
		ID:        "inv_plat1",
		CodeHash:  InviteCodeHash(code),
		CreatedBy: "usr_owner",
		Status:    "invited",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if mutate != nil {
		mutate(&inv)
	}
	if err := s.DB.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	return code
}

func TestRegisterWithRegistrationInvite(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code := seedRegistrationInvite(t, s, nil)

	actor, refresh, err := s.Register(ctx, RegisterInput{
		Email: "plat@example.com", Password: "hunter2safe", RegistrationCode: code,
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register with registration invite: %v", err)
	}
	if refresh == "" || !strings.HasPrefix(refresh, "atr_") {
		t.Fatalf("register must establish a session, refresh = %q", refresh)
	}
	if actor.Kind != "human" || actor.PlatformRole != "user" {
		t.Fatalf("registration invite must yield plain user, got kind=%s role=%s", actor.Kind, actor.PlatformRole)
	}
	// 关键语义：不入任何 workspace。
	var memberCount int64
	if err := s.DB.Model(&model.WorkspaceMember{}).Where("actor_id = ?", actor.ID).Count(&memberCount).Error; err != nil {
		t.Fatal(err)
	}
	if memberCount != 0 {
		t.Fatalf("registration invite must not add workspace membership, got %d", memberCount)
	}
	// 邀请已兑换；审计 invite.redeem（workspace_id 留空）+ auth.register。
	var inv model.RegistrationInvitation
	if err := s.DB.First(&inv, "id = ?", "inv_plat1").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "redeemed" || inv.RedeemedBy == nil || *inv.RedeemedBy != actor.ID {
		t.Fatalf("invitation not redeemed: %+v", inv)
	}
	var auditRows []model.AuditEntry
	if err := s.DB.Where("action = ?", "invite.redeem").Find(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(auditRows) != 1 || auditRows[0].WorkspaceID != nil {
		t.Fatalf("platform redeem audit must be server-level: %+v", auditRows)
	}
	// 事件：security.invite.redeemed，不带 workspace。
	var events []model.OutboxEvent
	if err := s.DB.Where("type = ?", outbox.TypeSecurityInviteRedeemed).Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("redeemed events = %d (err=%v)", len(events), err)
	}
	if events[0].WorkspaceID != nil {
		t.Fatalf("platform redeemed event must not carry workspace: %v", events[0].WorkspaceID)
	}
}

func TestRegistrationInviteReuseAndExpiry(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code := seedRegistrationInvite(t, s, nil)

	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "a@example.com", Password: "hunter2safe", RegistrationCode: code,
	}, "ip", "ua"); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	// 复用：同码二次兑换 → INVITE_INVALID。
	_, _, err := s.Register(ctx, RegisterInput{
		Email: "b@example.com", Password: "hunter2safe", RegistrationCode: code,
	}, "ip", "ua")
	apiErr := inviteErr(t, err)
	if apiErr.Code != httpx.CodeInviteInvalid {
		t.Fatalf("reuse must be INVITE_INVALID, got %s", apiErr.Code)
	}

	// 过期：invited 但已过 expires_at → INVITE_INVALID（防探测同文案）。
	expiredCode := seedRegistrationInvite(t, s, func(inv *model.RegistrationInvitation) {
		inv.ID = "inv_plat_exp"
		inv.ExpiresAt = time.Now().Add(-time.Hour)
	})
	_, _, err = s.Register(ctx, RegisterInput{
		Email: "c@example.com", Password: "hunter2safe", RegistrationCode: expiredCode,
	}, "ip", "ua")
	apiErr = inviteErr(t, err)
	if apiErr.Code != httpx.CodeInviteInvalid {
		t.Fatalf("expired must be INVITE_INVALID, got %s", apiErr.Code)
	}
}
