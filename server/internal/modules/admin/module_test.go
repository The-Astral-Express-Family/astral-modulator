package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// Round 34（TODO.md）：admin 用户管理——列表/停用/恢复/平台角色变更的
// 授权矩阵与停用后即时失效语义。

type fixture struct {
	m        *Module
	db       *gorm.DB
	svc      *auth.Service
	admin    *auth.Principal
	adminTok string
	user     *auth.Principal
	userTok  string
	agentP   *auth.Principal
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, discardLogger())
	t.Cleanup(svc.DrainBackgroundWrites) // 先于关库/TempDir 清理 drain 后台写（cleanup LIFO）
	m := &Module{DB: db, Auth: svc, Log: discardLogger()}

	// 首个 human = 平台 admin（registerBootstrap 授予）。
	adminActor, refresh, err := svc.Register(ctx, auth.RegisterInput{
		Email: "admin@example.com", Password: "hunter2safe", DisplayName: "Admin",
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	pair, err := svc.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh admin: %v", err)
	}

	// 第二个 human 直插（registerBootstrap 的 cold-start 守卫拦第二次注册）。
	hash, err := auth.HashPassword("hunter2safe")
	if err != nil {
		t.Fatal(err)
	}
	userActor := &model.Actor{ID: "usr_user", Kind: "human", PlatformRole: "user", DisplayName: "User"}
	if err := db.Create(userActor).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HumanAuth{ActorID: userActor.ID, Email: "user@example.com", PasswordHash: hash}).Error; err != nil {
		t.Fatal(err)
	}
	userRefresh, _, err := svc.Login(ctx, "user@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	userPair, err := svc.Refresh(ctx, userRefresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh user: %v", err)
	}

	// agent + credential：验证 credential 主体同样过不了 platform:users:*。
	agent := &model.Actor{ID: "agt_ad1", Kind: "agent", PlatformRole: "agent", DisplayName: "A"}
	if err := db.Create(agent).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewCredentialSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Credential{
		ID: "cred_ad1", ActorID: agent.ID, Kind: "agent",
		SecretHash: auth.HashToken(secret), Scopes: `["task:read"]`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	agentP, apiErr := svc.ResolvePrincipal(ctx, secret, "")
	if apiErr != nil {
		t.Fatalf("resolve agent credential: %v", apiErr)
	}

	return &fixture{
		m: m, db: db, svc: svc,
		admin:    &auth.Principal{ActorID: adminActor.ID, Kind: "human", PlatformRole: adminActor.PlatformRole},
		adminTok: pair.AccessToken,
		user:     &auth.Principal{ActorID: userActor.ID, Kind: "human", PlatformRole: "user"},
		userTok:  userPair.AccessToken,
		agentP:   agentP,
	}
}

func (f *fixture) call(t *testing.T, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rest := strings.TrimPrefix(path, "/api/v1/admin/users/")
	rctx.URLParams.Add("actor_id", strings.Split(rest, "/")[0])
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithPrincipal(ctx, p)
	switch {
	case strings.HasSuffix(path, "/disable"):
		f.m.disableUser(rec, req.WithContext(ctx))
	case strings.HasSuffix(path, "/enable"):
		f.m.enableUser(rec, req.WithContext(ctx))
	case strings.HasSuffix(path, "/role"):
		f.m.changeRole(rec, req.WithContext(ctx))
	case strings.HasSuffix(path, "/users"):
		f.m.listUsers(rec, req.WithContext(ctx))
	default:
		t.Fatalf("unroutable %s %q", method, path)
	}
	return rec
}

func TestAdminUserListMatrix(t *testing.T) {
	f := newFixture(t)

	// admin：200，两项 human，带邮箱与平台角色。
	rec := f.call(t, f.admin, "GET", "/api/v1/admin/users", "")
	if rec.Code != 200 {
		t.Fatalf("admin list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			ID           string `json:"id"`
			PlatformRole string `json:"platform_role"`
			Email        string `json:"email"`
			DisabledAt   any    `json:"disabled_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2 (agent 不入平台用户列表)", len(page.Items))
	}
	adminSeen := false
	for _, it := range page.Items {
		if it.Email == "" {
			t.Fatalf("email missing for %s", it.ID)
		}
		if it.DisabledAt != nil {
			t.Fatalf("disabled_at should be null for %s", it.ID)
		}
		if it.Email == "admin@example.com" {
			adminSeen = it.PlatformRole == "admin"
		}
		if it.ID == "usr_user" && it.PlatformRole != "user" {
			t.Fatalf("user platform_role = %q", it.PlatformRole)
		}
	}
	if !adminSeen {
		t.Fatalf("admin account missing or mislabeled: %s", rec.Body.String())
	}

	// user 与 agent credential 主体 → 403。
	for name, p := range map[string]*auth.Principal{"user": f.user, "agent": f.agentP} {
		rec := f.call(t, p, "GET", "/api/v1/admin/users", "")
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), "INSUFFICIENT_SCOPE") {
			t.Fatalf("%s list status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestDisableBlocksAuthImmediately(t *testing.T) {
	f := newFixture(t)

	// 非 admin 不能停用。
	rec := f.call(t, f.user, "POST", "/api/v1/admin/users/usr_user/disable", "")
	if rec.Code != 403 {
		t.Fatalf("user disable status = %d", rec.Code)
	}

	// admin 停用 user → 204；重复停用 → 409。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/disable", "")
	if rec.Code != 204 {
		t.Fatalf("admin disable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/disable", "")
	if rec.Code != 409 {
		t.Fatalf("second disable status = %d", rec.Code)
	}

	// 在途 access token 立即失效：会话已被 RevokeActorSessions 撤销
	// （报 session revoked）；即便撤销失败，authActor 的 DisabledAt 检查
	// 也兜底报 account disabled——两条路都是 401。
	if _, apiErr := f.svc.ResolvePrincipal(context.Background(), f.userTok, ""); apiErr == nil || apiErr.Status != 401 {
		t.Fatalf("disabled access principal: %+v %v", apiErr, apiErr)
	}
	// 登录也被拒（DisabledAt 闸门）。
	if _, _, err := f.svc.Login(context.Background(), "user@example.com", "hunter2safe", "ip", "ua"); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled login: %v", err)
	}

	// 恢复后可登录。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/enable", "")
	if rec.Code != 204 {
		t.Fatalf("enable status = %d", rec.Code)
	}
	if _, _, err := f.svc.Login(context.Background(), "user@example.com", "hunter2safe", "ip", "ua"); err != nil {
		t.Fatalf("login after enable: %v", err)
	}

	// 自停用禁止。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/"+f.admin.ActorID+"/disable", "")
	if rec.Code != 400 {
		t.Fatalf("self disable status = %d", rec.Code)
	}
	// 非人类目标拒绝。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/agt_ad1/disable", "")
	if rec.Code != 400 {
		t.Fatalf("agent disable status = %d", rec.Code)
	}
	// 不存在的用户 → 404。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_ghost/disable", "")
	if rec.Code != 404 {
		t.Fatalf("ghost disable status = %d", rec.Code)
	}
}

func TestChangeRoleGuards(t *testing.T) {
	f := newFixture(t)

	// 提升为 admin → 200 + 返回新角色。
	rec := f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/role", `{"platform_role":"admin"}`)
	if rec.Code != 200 {
		t.Fatalf("promote status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"platform_role":"admin"`) {
		t.Fatalf("promote body = %s", rec.Body.String())
	}
	// 变更即时生效：同 actor 的新认证解析出 admin。
	var actor model.Actor
	if err := f.db.First(&actor, "id = ?", "usr_user").Error; err != nil {
		t.Fatal(err)
	}
	if actor.PlatformRole != "admin" {
		t.Fatalf("db platform_role = %q", actor.PlatformRole)
	}

	// 非法值 / 非人类 / 自改 / 非法权。
	for _, c := range []struct {
		body, target string
		want         int
	}{
		{`{"platform_role":"root"}`, "usr_user", 400},
		{`{"platform_role":"admin"}`, "agt_ad1", 400},
		{`{"platform_role":"user"}`, f.admin.ActorID, 400},
	} {
		rec := f.call(t, f.admin, "POST", "/api/v1/admin/users/"+c.target+"/role", c.body)
		if rec.Code != c.want {
			t.Fatalf("role change %s %s status = %d, body = %s", c.body, c.target, rec.Code, rec.Body.String())
		}
	}

	// 非 admin → 403。
	rec = f.call(t, f.user, "POST", "/api/v1/admin/users/usr_user/role", `{"platform_role":"admin"}`)
	if rec.Code != 403 {
		t.Fatalf("user role change status = %d", rec.Code)
	}

	// 降回 user（幂等冲突另测）。
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/role", `{"platform_role":"user"}`)
	if rec.Code != 200 {
		t.Fatalf("demote status = %d", rec.Code)
	}
	rec = f.call(t, f.admin, "POST", "/api/v1/admin/users/usr_user/role", `{"platform_role":"user"}`)
	if rec.Code != 409 {
		t.Fatalf("unchanged role status = %d", rec.Code)
	}
}
