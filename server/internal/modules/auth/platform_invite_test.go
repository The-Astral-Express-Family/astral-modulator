// 平台级邀请兑换（00018）：registerWithInvite 先查 platform_invitations，
// 命中→普通 user、不入任何 workspace；未命中回退 workspace_invitations。
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

// seedPlatformInvite 直插一张平台邀请，返回明文码。
func seedPlatformInvite(t *testing.T, s *Service, mutate func(*model.PlatformInvitation)) string {
	t.Helper()
	code, err := NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	inv := model.PlatformInvitation{
		ID:        "inv_plat1",
		CodeHash:  HashToken(NormalizeInviteCode(code)),
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

func TestRegisterWithPlatformInvite(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code := seedPlatformInvite(t, s, nil)

	actor, refresh, err := s.Register(ctx, RegisterInput{
		Email: "plat@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register with platform invite: %v", err)
	}
	if refresh == "" || !strings.HasPrefix(refresh, "atr_") {
		t.Fatalf("register must establish a session, refresh = %q", refresh)
	}
	if actor.Kind != "human" || actor.PlatformRole != "user" {
		t.Fatalf("platform invite must yield plain user, got kind=%s role=%s", actor.Kind, actor.PlatformRole)
	}
	// 关键语义：不入任何 workspace。
	var memberCount int64
	if err := s.DB.Model(&model.WorkspaceMember{}).Where("actor_id = ?", actor.ID).Count(&memberCount).Error; err != nil {
		t.Fatal(err)
	}
	if memberCount != 0 {
		t.Fatalf("platform invite must not add workspace membership, got %d", memberCount)
	}
	// 邀请已兑换；审计 invite.redeem（workspace_id 留空）+ auth.register。
	var inv model.PlatformInvitation
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

func TestPlatformInviteReuseAndExpiry(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code := seedPlatformInvite(t, s, nil)

	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "a@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua"); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	// 复用：同码二次兑换 → INVITE_INVALID。
	_, _, err := s.Register(ctx, RegisterInput{
		Email: "b@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	apiErr := inviteErr(t, err)
	if apiErr.Code != httpx.CodeInviteInvalid {
		t.Fatalf("reuse must be INVITE_INVALID, got %s", apiErr.Code)
	}

	// 过期：invited 但已过 expires_at → INVITE_INVALID（防探测同文案）。
	expiredCode := seedPlatformInvite(t, s, func(inv *model.PlatformInvitation) {
		inv.ID = "inv_plat_exp"
		inv.ExpiresAt = time.Now().Add(-time.Hour)
	})
	_, _, err = s.Register(ctx, RegisterInput{
		Email: "c@example.com", Password: "hunter2safe", InviteCode: expiredCode,
	}, "ip", "ua")
	apiErr = inviteErr(t, err)
	if apiErr.Code != httpx.CodeInviteInvalid {
		t.Fatalf("expired must be INVITE_INVALID, got %s", apiErr.Code)
	}
}

// 平台码与 workspace 码互不干扰：workspace 码仍走原路径。
func TestWorkspaceInviteStillRedeems(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, inv := seedInviteWorld(t, s, "contributor", nil)

	actor, _, err := s.Register(ctx, RegisterInput{
		Email: "ws@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("workspace invite redeem: %v", err)
	}
	var member model.WorkspaceMember
	if err := s.DB.First(&member, "workspace_id = ? AND actor_id = ?", inv.WorkspaceID, actor.ID).Error; err != nil {
		t.Fatalf("workspace invite must add membership: %v", err)
	}
	if member.Role != "contributor" {
		t.Fatalf("membership role = %q", member.Role)
	}
}
