package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	return NewService(testsupport.NewTestDB(t), discardLogger())
}

func TestRegisterBootstrapOnly(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	actor, err := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe", DisplayName: "Hime"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if actor.Kind != "human" || !strings.HasPrefix(actor.ID, "usr_") {
		t.Fatalf("unexpected actor: %+v", actor)
	}
	// 第二次注册必须被拒（bootstrap-only）。
	if _, err := s.Register(ctx, RegisterInput{Email: "other@example.com", Password: "hunter2safe"}); err == nil {
		t.Fatal("second register should fail")
	}
	// 弱口令拒绝。
	if _, err := s.Register(ctx, RegisterInput{Email: "x@y.com", Password: "short"}); err == nil {
		t.Fatal("weak password should fail")
	}
}

func TestLoginAndSessionRefreshRotation(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"})

	// 错误口令 → 401 形状。
	if _, _, err := s.Login(ctx, "human@example.com", "wrong-pass1", "ip", "ua"); err == nil {
		t.Fatal("bad password should fail")
	}

	refresh, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.HasPrefix(refresh, "atr_") {
		t.Fatalf("refresh prefix: %q", refresh)
	}

	// access token 可解析。
	pair, err := s.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	p, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, "")
	if apiErr != nil || p.ActorID != actor.ID {
		t.Fatalf("access token principal: %+v err=%v", p, apiErr)
	}

	// 旧 refresh 重放 → 整族撤销。
	if _, err := s.Refresh(ctx, refresh, "ip", "ua"); err == nil {
		t.Fatal("replayed refresh should fail")
	}
	// 重放后，连新 refresh 也失效（family revoked）。
	if _, err := s.Refresh(ctx, pair.RefreshToken, "ip", "ua"); err == nil {
		t.Fatal("family should be revoked after replay")
	}
	// access token 也不可用。
	if _, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, ""); apiErr == nil || apiErr.Code != httpx.CodeTokenRevoked {
		t.Fatalf("access after family revoke: %+v %v", apiErr, apiErr)
	}
}

func TestDeviceFlow(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"})

	created, err := s.CreateDeviceAuthorization(ctx, "cli", "https://astral.example.com")
	if err != nil {
		t.Fatalf("create device auth: %v", err)
	}
	if created.VerificationURI != "https://astral.example.com/device" {
		t.Fatalf("verification uri: %q", created.VerificationURI)
	}

	// pending → AUTHORIZATION_PENDING。
	_, err = s.ExchangeDeviceToken(ctx, created.DeviceCode, "ip", "ua")
	apiErr, ok := err.(*httpx.APIError)
	if !ok || apiErr.Code != httpx.CodeAuthorizationPending {
		t.Fatalf("pending exchange: %v", err)
	}

	// 浏览器侧按 user_code 查询 + 批准。
	view, err := s.FindByUserCode(ctx, created.UserCode)
	if err != nil || view.Status != "pending" {
		t.Fatalf("find by user_code: %+v %v", view, err)
	}
	if err := s.Approve(ctx, view.ID, actor.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// 兑换 → token pair；重放兑换 → 拒绝。
	pair, err := s.ExchangeDeviceToken(ctx, created.DeviceCode, "ip", "ua")
	if err != nil || pair.ActorID != actor.ID {
		t.Fatalf("exchange: %v", err)
	}
	if _, err := s.ExchangeDeviceToken(ctx, created.DeviceCode, "ip", "ua"); err == nil {
		t.Fatal("device_code reuse should fail")
	}

	// deny 流程。
	created2, _ := s.CreateDeviceAuthorization(ctx, "cli", "https://x.example.com")
	view2, _ := s.FindByUserCode(ctx, created2.UserCode)
	if err := s.Deny(ctx, view2.ID, actor.ID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if _, err := s.ExchangeDeviceToken(ctx, created2.DeviceCode, "ip", "ua"); err == nil {
		t.Fatal("denied exchange should fail")
	}
}

func TestCredentialLifecycle(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"})
	agent := &model.Actor{ID: "agt_test1", Kind: "agent", DisplayName: "Coder"}
	if err := s.DB.Create(agent).Error; err != nil {
		t.Fatal(err)
	}
	ws := "ws_test1"

	issued, err := s.IssueCredential(ctx, CreateCredentialInput{
		ActorID: agent.ID, Kind: "agent",
		Scopes:    []string{ScopeTaskRead, ScopeTaskClaim},
		Workspace: &ws, CreatedBy: actor.ID,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(issued.Secret, CredentialPrefix) {
		t.Fatalf("secret format: %q", issued.Secret)
	}

	// credential 解析 + workspace scope 生效。
	p, apiErr := s.ResolvePrincipal(ctx, issued.Secret, "")
	if apiErr != nil || p.Kind != "agent" {
		t.Fatalf("resolve credential: %+v %v", p, apiErr)
	}
	scopes, err := s.WorkspaceScopes(ctx, p, ws)
	if err != nil || !scopes[ScopeTaskClaim] || scopes[ScopeTaskWrite] {
		t.Fatalf("scopes: %+v err=%v", scopes, err)
	}
	// 未绑定 workspace → 空。
	other, _ := s.WorkspaceScopes(ctx, p, "ws_other")
	if len(other) != 0 {
		t.Fatalf("unbound workspace scopes: %+v", other)
	}

	// 吊销后立即失效。
	if err := s.RevokeCredential(ctx, issued.CredentialID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, apiErr := s.ResolvePrincipal(ctx, issued.Secret, ""); apiErr == nil || apiErr.Code != httpx.CodeTokenRevoked {
		t.Fatalf("revoked credential: %v", apiErr)
	}
}

func TestHumanCookieSession(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"})
	refresh, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	p, apiErr := s.ResolvePrincipal(ctx, "", refresh)
	if apiErr != nil || p.ActorID != actor.ID || !p.IsHuman() {
		t.Fatalf("cookie principal: %+v %v", p, apiErr)
	}
	_ = s.Logout(ctx, refresh)
	if _, apiErr := s.ResolvePrincipal(ctx, "", refresh); apiErr == nil {
		t.Fatal("cookie session should be revoked after logout")
	}
}

func TestUserCodeShape(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := NewUserCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("bad user code: %q", code)
		}
		for _, c := range code {
			if c == 'O' || c == '0' || c == '1' || c == 'I' || c == 'L' {
				t.Fatalf("ambiguous char in %q", code)
			}
		}
	}
}

func TestSessionExpiry(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"})
	agent := &model.Actor{ID: "agt_x1", Kind: "agent", DisplayName: "A"}
	s.DB.Create(agent)

	// 过期 access token。
	refresh, _, _ := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	pair, _ := s.Refresh(ctx, refresh, "ip", "ua")
	var sess model.Session
	s.DB.Where("actor_id = ?", actor.ID).First(&sess)
	past := time.Now().Add(-time.Minute)
	s.DB.Model(&model.Session{}).Where("id = ?", sess.ID).Update("access_expires_at", past)
	if _, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, ""); apiErr == nil || apiErr.Code != httpx.CodeTokenExpired {
		t.Fatalf("expired access: %v", apiErr)
	}
}
