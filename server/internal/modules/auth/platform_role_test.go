package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// Round 33（TODO.md）：平台角色基建。首个 human 即 admin、邀请注册是 user、
// credential 主体固化 kind、RequireGlobal 按 GlobalScopesFor 判定。

func TestFirstHumanIsPlatformAdmin(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	actor, refresh, err := s.Register(ctx, RegisterInput{Email: "root@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if actor.PlatformRole != "admin" {
		t.Fatalf("first human platform_role = %q, want admin", actor.PlatformRole)
	}

	// 登录路径返回的 actor 同样是 admin。
	_, loginActor, err := s.Login(ctx, "root@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginActor.PlatformRole != "admin" {
		t.Fatalf("login platform_role = %q", loginActor.PlatformRole)
	}

	// access token 与 cookie 两条认证管线都解析出平台角色。
	pair, err := s.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if p, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, ""); apiErr != nil || p.PlatformRole != "admin" {
		t.Fatalf("access principal: %+v err=%v", p, apiErr)
	}
	// cookie 管线认当前轮换后的 refresh token。
	if p, apiErr := s.ResolvePrincipal(ctx, "", pair.RefreshToken); apiErr != nil || p.PlatformRole != "admin" {
		t.Fatalf("cookie principal: %+v err=%v", p, apiErr)
	}
}

func TestInviteRegistrationPlatformRoleUser(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	code, _ := seedInviteWorld(t, s, "contributor", nil)

	actor, _, err := s.Register(ctx, RegisterInput{
		Email: "invited@example.com", Password: "hunter2safe", InviteCode: code,
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if actor.PlatformRole != "user" {
		t.Fatalf("invited human platform_role = %q, want user", actor.PlatformRole)
	}
}

func TestCredentialPrincipalPlatformRoleIsKind(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()

	agent := &model.Actor{ID: "agt_pr1", Kind: "agent", PlatformRole: "agent", DisplayName: "A"}
	if err := s.DB.Create(agent).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := NewCredentialSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&model.Credential{
		ID: "cred_pr1", ActorID: agent.ID, Kind: "agent",
		SecretHash: HashToken(secret), Scopes: `["task:read"]`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	p, apiErr := s.ResolvePrincipal(ctx, secret, "")
	if apiErr != nil {
		t.Fatalf("resolve: %v", apiErr)
	}
	if p.PlatformRole != "agent" || p.Kind != "agent" {
		t.Fatalf("credential principal role/kind = %q/%q", p.PlatformRole, p.Kind)
	}
	// agent 永无平台 scope（GlobalScopesFor["agent"] 为空）。
	if apiErr := RequireGlobal(principalRequest(t, p), ScopePlatformCredsManage); apiErr == nil {
		t.Fatal("agent must not hold platform scopes")
	}
}

func TestRequireGlobalMatrix(t *testing.T) {
	cases := []struct {
		role string
		pass bool
	}{
		{"admin", true},
		{"user", false},
		{"agent", false},
		{"service", false},
		{"", false},
	}
	for _, c := range cases {
		p := &Principal{ActorID: "usr_x", Kind: "human", PlatformRole: c.role}
		for _, scope := range []string{ScopePlatformUsersRead, ScopePlatformUsersManage, ScopePlatformCredsManage} {
			apiErr := RequireGlobal(principalRequest(t, p), scope)
			if c.pass && apiErr != nil {
				t.Fatalf("role %q scope %q: unexpected %v", c.role, scope, apiErr)
			}
			if !c.pass {
				if apiErr == nil {
					t.Fatalf("role %q scope %q: expected 403", c.role, scope)
				}
				if apiErr.Status != http.StatusForbidden || apiErr.Code != httpx.CodeInsufficientScope {
					t.Fatalf("role %q scope %q: got %+v", c.role, scope, apiErr)
				}
			}
		}
	}
}

func principalRequest(t *testing.T, p *Principal) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return req.WithContext(WithPrincipal(req.Context(), p))
}

// 平台角色与 kind 的一致性由建号点保证；这里锁一条时间基准防止 DisabledAt
// 语义被误用（round 34 启用，NULL = 正常）。
func TestActorDisabledAtDefaultNil(t *testing.T) {
	s := newSvc(t)
	actor, _, err := s.Register(context.Background(), RegisterInput{Email: "a@b.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	if actor.DisabledAt != nil {
		t.Fatalf("DisabledAt should default nil, got %v", actor.DisabledAt)
	}
	if time.Time.IsZero(actor.CreatedAt) {
		t.Fatal("CreatedAt should be set")
	}
}
