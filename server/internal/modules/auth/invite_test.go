// 注册端点契约门（ADR-0009 双轨分离）：注册只认注册邀请码；工作区邀请码
// 与一切未知码在 /auth/register 一律 INVITE_INVALID（同码同文案防探测），
// 不消耗、不留 actor 残留。注册码的边界语义（统一文案/失败不消耗/归一化）
// 也归本文件；兑换建号主路径见 registration_invite_test.go。
package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// seedInviteWorld 造一张有效的工作区邀请（契约门测试的诱饵码）：
// 签发人、workspace、签发人成员行齐备。前置行按 workspace 存在性幂等。
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
		CodeHash:    InviteCodeHash(code),
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

// TestRegisterRejectsWorkspaceCode 是双轨分离的契约门：有效的工作区邀请码
// 在注册端点也必须被拒——工作区码是入伙资格，不是注册资格。
// （取代旧 TestWorkspaceInviteStillRedeems / TestRegisterWithInviteCode：
// 断言变更有据——用户裁决注册/工作区邀请必须分离，见行为 delta log。）
func TestRegisterRejectsWorkspaceCode(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "contributor", nil)

	_, _, err := s.Register(ctx, RegisterInput{
		Email: "gate@example.com", Password: "hunter2safe", RegistrationCode: code,
	}, "ip", "ua")
	apiErr := inviteErr(t, err)
	if apiErr.Code != httpx.CodeInviteInvalid || apiErr.Status != 400 {
		t.Fatalf("workspace code at register must be 400 INVITE_INVALID, got %+v", apiErr)
	}
	// 码不被消耗；无 actor 残留（只剩诱饵世界的 usr_owner）。
	var inv model.Invitation
	if err := s.DB.First(&inv, "id = ?", "inv_test1").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("workspace invitation must stay invited, got %q", inv.Status)
	}
	var actors int64
	if err := s.DB.Model(&model.Actor{}).Where("kind = ?", "human").Count(&actors).Error; err != nil {
		t.Fatal(err)
	}
	if actors != 1 {
		t.Fatalf("no actor may be created by a rejected register, humans = %d", actors)
	}
}

// 防探测：查无/已撤销/已过期/畸形/工作区码五种失效必须同码同文案。
func TestRegistrationCodeInvalidUnified(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	wsCode, _ := seedInviteWorld(t, s, "viewer", nil)

	revoked := seedRegistrationInvite(t, s, nil)
	expired := seedRegistrationInvite(t, s, func(inv *model.RegistrationInvitation) {
		inv.ID = "reg_exp"
		inv.ExpiresAt = time.Now().Add(-time.Hour)
	})
	if err := s.DB.Model(&model.RegistrationInvitation{}).
		Where("code_hash = ?", InviteCodeHash(revoked)).
		Update("status", "revoked").Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   RegisterInput
	}{
		{"unknown", RegisterInput{Email: "a@example.com", Password: "hunter2safe", RegistrationCode: "AAAAA-AAAAA-AAAAA-AAAAA"}},
		{"revoked", RegisterInput{Email: "b@example.com", Password: "hunter2safe", RegistrationCode: revoked}},
		{"expired", RegisterInput{Email: "c@example.com", Password: "hunter2safe", RegistrationCode: expired}},
		{"malformed", RegisterInput{Email: "d@example.com", Password: "hunter2safe", RegistrationCode: "short"}},
		{"workspace_code", RegisterInput{Email: "e@example.com", Password: "hunter2safe", RegistrationCode: wsCode}},
	}
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

// 失败不消耗：email 撞车（409 EMAIL_TAKEN 整体回滚）与弱口令（400，码校验
// 先于口令策略之后的守卫）后，注册码必须仍是 invited。
func TestRegistrationCodeNotConsumedOnFailure(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	preCode := seedRegistrationInvite(t, s, nil)
	code := seedRegistrationInvite(t, s, func(inv *model.RegistrationInvitation) {
		inv.ID = "reg_second"
	})

	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "taken@example.com", Password: "hunter2safe", RegistrationCode: preCode,
	}, "ip", "ua"); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	_, _, err := s.Register(ctx, RegisterInput{
		Email: "taken@example.com", Password: "hunter2safe", RegistrationCode: code,
	}, "ip", "ua")
	if apiErr := inviteErr(t, err); apiErr.Code != httpx.CodeEmailTaken || apiErr.Status != 409 {
		t.Fatalf("email taken: %+v", apiErr)
	}
	var inv model.RegistrationInvitation
	if err := s.DB.First(&inv, "id = ?", "reg_second").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("registration code must stay invited after EMAIL_TAKEN, got %q", inv.Status)
	}

	// 弱口令同样不消耗注册码。
	_, _, err = s.Register(ctx, RegisterInput{Email: "weak@example.com", Password: "short", RegistrationCode: code}, "ip", "ua")
	if apiErr := inviteErr(t, err); apiErr.Code != httpx.CodeValidationFailed {
		t.Fatalf("weak password: %+v", apiErr)
	}
	if err := s.DB.First(&inv, "id = ?", "reg_second").Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("registration code must stay invited after weak password, got %q", inv.Status)
	}
}

func TestRegistrationCodeNormalization(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code := seedRegistrationInvite(t, s, nil)

	lower := strings.ToLower(code)
	if _, _, err := s.Register(ctx, RegisterInput{
		Email: "norm@example.com", Password: "hunter2safe", RegistrationCode: lower,
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
