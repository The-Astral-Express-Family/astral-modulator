package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// seedInviteWorld 造一个可用邀请的全部前置：签发人、workspace、签发人成员行。
// 前置行（签发人/workspace/成员行）按 workspace 存在性幂等，可多次调用。
func seedInviteWorld(t *testing.T, s *Service, role string, mutate func(*model.Invitation)) (string, model.Invitation) {
	t.Helper()
	var existing model.Workspace
	if err := s.DB.First(&existing, "id = ?", "ws_inv").Error; err != nil {
		owner := &model.Actor{ID: "usr_owner", Kind: "human", DisplayName: "Owner"}
		if err := s.DB.Create(owner).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Create(&model.Workspace{ID: "ws_inv", Name: "inv", Slug: "inv", CreatedBy: owner.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Create(&model.WorkspaceMember{WorkspaceID: "ws_inv", ActorID: owner.ID, Role: "owner"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	code, err := NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	inv := model.Invitation{
		ID:          "inv_test1",
		WorkspaceID: "ws_inv",
		Role:        role,
		CodeHash:    HashToken(NormalizeInviteCode(code)),
		CreatedBy:   "usr_owner",
		Status:      "invited",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	if mutate != nil {
		mutate(&inv)
	}
	if err := s.DB.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	return code, inv
}

func inviteErr(t *testing.T, err error) *httpx.APIError {
	t.Helper()
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *httpx.APIError, got %v", err)
	}
	return apiErr
}

func TestRegisterWithInviteCode(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "contributor", nil)

	actor, refresh, err := s.Register(ctx, RegisterInput{
		Email: "new@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register with invite: %v", err)
	}
	if refresh == "" || !strings.HasPrefix(refresh, "atr_") {
		t.Fatalf("register must establish a web session, refresh = %q", refresh)
	}
	var mem model.WorkspaceMember
	if err := s.DB.First(&mem, "workspace_id = ? AND actor_id = ?", "ws_inv", actor.ID).Error; err != nil {
		t.Fatalf("membership row missing: %v", err)
	}
	if mem.Role != "contributor" {
		t.Fatalf("membership role = %q", mem.Role)
	}
	var inv model.Invitation
	if err := s.DB.First(&inv, "id = ?", "inv_test1").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "redeemed" || inv.RedeemedBy == nil || *inv.RedeemedBy != actor.ID {
		t.Fatalf("invitation not redeemed properly: %+v", inv)
	}
	var audits []model.AuditEntry
	if err := s.DB.Where("action IN ?", []string{"invite.redeem", "auth.register"}).Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 {
		t.Fatalf("audit rows = %d, want invite.redeem + auth.register", len(audits))
	}
	var ev model.OutboxEvent
	if err := s.DB.First(&ev, "type = ?", outbox.TypeSecurityInviteRedeemed).Error; err != nil {
		t.Fatalf("outbox event missing: %v", err)
	}

	// 同码重用：邀请已兑换 → INVITE_INVALID。
	_, _, err = s.Register(ctx, RegisterInput{Email: "again@example.com", Password: "hunter2safe", InviteCode: code}, "ip", "ua")
	if apiErr := inviteErr(t, err); apiErr.Code != httpx.CodeInviteInvalid {
		t.Fatalf("reuse code = %s (%s)", apiErr.Code, apiErr.Message)
	}
}

// 防探测：四种失效 + 查无必须同码同文案。
func TestInviteInvalidUnified(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "viewer", nil)

	cases := []struct {
		name string
		in   RegisterInput
	}{
		{"unknown", RegisterInput{Email: "a@example.com", Password: "hunter2safe", InviteCode: "AAAAA-AAAAA-AAAAA-AAAAA"}},
		{"revoked", RegisterInput{Email: "b@example.com", Password: "hunter2safe", InviteCode: code}},
		{"expired", RegisterInput{Email: "c@example.com", Password: "hunter2safe"}},
		{"malformed", RegisterInput{Email: "d@example.com", Password: "hunter2safe", InviteCode: "short"}},
	}
	var inv model.Invitation
	if err := s.DB.First(&inv, "id = ?", "inv_test1").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&model.Invitation{}).Where("id = ?", inv.ID).Update("status", "revoked").Error; err != nil {
		t.Fatal(err)
	}

	code2, _ := seedInviteWorld(t, s, "viewer", func(inv *model.Invitation) {
		inv.ID = "inv_test2"
		inv.ExpiresAt = time.Now().Add(-time.Hour)
	})
	cases[2].in.InviteCode = code2

	var wantMsg string
	for _, tc := range cases {
		name, in := tc.name, tc.in
		_, _, err := s.Register(ctx, in, "ip", "ua")
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
		apiErr := inviteErr(t, err)
		if apiErr.Code != httpx.CodeInviteInvalid {
			t.Fatalf("%s: code = %s", name, apiErr.Code)
		}
		if wantMsg == "" {
			wantMsg = apiErr.Message
			continue
		}
		if apiErr.Message != wantMsg {
			t.Fatalf("%s: message %q differs from %q (anti-enumeration)", name, apiErr.Message, wantMsg)
		}
	}
}

func TestInviteRedeemDoesNotConsumeOnFailure(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "maintainer", nil)

	// email 撞车：先用另一张邀请占用邮箱，再持新码用同邮箱兑换 → 409，邀请不消耗。
	preCode, _ := seedInviteWorld(t, s, "viewer", func(inv *model.Invitation) {
		inv.ID = "inv_test0"
	})
	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "taken@example.com", Password: "hunter2safe", InviteCode: preCode,
	}, "ip", "ua"); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	_, _, err := s.Register(ctx, RegisterInput{
		Email: "taken@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	if apiErr := inviteErr(t, err); apiErr.Code != httpx.CodeEmailTaken || apiErr.Status != 409 {
		t.Fatalf("email taken: %+v", apiErr)
	}
	var inv model.Invitation
	if err := s.DB.First(&inv, "id = ?", "inv_test1").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("invitation must stay invited after EMAIL_TAKEN, got %q", inv.Status)
	}

	// 弱口令同样不消耗邀请。
	_, _, err = s.Register(ctx, RegisterInput{Email: "weak@example.com", Password: "short", InviteCode: code}, "ip", "ua")
	if apiErr := inviteErr(t, err); apiErr.Code != httpx.CodeValidationFailed {
		t.Fatalf("weak password: %+v", apiErr)
	}
	if err := s.DB.First(&inv, "id = ?", "inv_test1").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("invitation must stay invited after weak password, got %q", inv.Status)
	}
}

func TestInviteCodeNormalization(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "viewer", nil)

	lower := strings.ToLower(code)
	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "norm@example.com", Password: "hunter2safe", InviteCode: lower,
	}, "ip", "ua"); err != nil {
		t.Fatalf("normalized code should redeem: %v", err)
	}

	// 形状：20 位 Crockford base32 + 4 个分隔符；字符表无 I/L/O/U。
	fresh, err := NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	grouped := strings.Split(fresh, "-")
	if len(grouped) != 4 {
		t.Fatalf("grouping: %q", fresh)
	}
	stripped := strings.ReplaceAll(fresh, "-", "")
	if len(stripped) != 20 || strings.ToUpper(stripped) != stripped {
		t.Fatalf("shape: %q", fresh)
	}
	if strings.ContainsAny(stripped, "ILOU") {
		t.Fatalf("confusable char in %q", fresh)
	}
}
