package workspace

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

// Round 33（TODO.md）：全局（不绑定 workspace）credential 的签发/吊销从
// 「任意 human」收口为「平台 admin」（platform:credentials:manage）。
// 绑定 workspace 的签发路径不受影响，仍走 agent:manage。

type credFixture struct {
	m     *Module
	db    *gorm.DB
	agent *model.Actor
	admin *auth.Principal
	user  *auth.Principal
}

func credSetup(t *testing.T) *credFixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m := &Module{DB: db, Auth: svc, WebBaseURL: "https://app.example.com"}

	adminActor := &model.Actor{ID: "usr_admin", Kind: "human", PlatformRole: "admin", DisplayName: "Admin"}
	userActor := &model.Actor{ID: "usr_user", Kind: "human", PlatformRole: "user", DisplayName: "User"}
	agent := &model.Actor{ID: "agt_cr1", Kind: "agent", PlatformRole: "agent", DisplayName: "A"}
	for _, a := range []*model.Actor{adminActor, userActor, agent} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &credFixture{
		m: m, db: db, agent: agent,
		admin: &auth.Principal{ActorID: adminActor.ID, Kind: "human", PlatformRole: "admin"},
		user:  &auth.Principal{ActorID: userActor.ID, Kind: "human", PlatformRole: "user"},
	}
}

// callCredential 按 chi 生产路由的等价方式分发凭证端点。
func (f *credFixture) callCredential(t *testing.T, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rest := strings.TrimPrefix(path, "/api/v1/agents/")
	rctx.URLParams.Add("agent_id", f.agent.ID)
	if i := strings.Index(rest, "/credentials/"); i >= 0 {
		rctx.URLParams.Add("credential_id", strings.TrimPrefix(rest[i+len("/credentials/"):], ""))
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithPrincipal(ctx, p)
	switch {
	case method == "POST":
		f.m.createCredential(rec, req.WithContext(ctx))
	case method == "DELETE":
		f.m.revokeCredential(rec, req.WithContext(ctx))
	default:
		t.Fatalf("unroutable %s %q", method, path)
	}
	return rec
}

func TestGlobalCredentialAdminOnly(t *testing.T) {
	f := credSetup(t)
	body := `{"scopes":["task:read"]}`

	// 普通 human → 403 INSUFFICIENT_SCOPE（收口前的 MVP 行为是放行）。
	rec := f.callCredential(t, f.user, "POST", "/api/v1/agents/agt_cr1/credentials", body)
	if rec.Code != 403 {
		t.Fatalf("user issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "INSUFFICIENT_SCOPE") {
		t.Fatalf("expected INSUFFICIENT_SCOPE, body = %s", rec.Body.String())
	}

	// 平台 admin → 201，明文 secret 只出现一次。
	rec = f.callCredential(t, f.admin, "POST", "/api/v1/agents/agt_cr1/credentials", body)
	if rec.Code != 201 {
		t.Fatalf("admin issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var issued struct {
		CredentialID string `json:"credential_id"`
		Secret       string `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.CredentialID == "" || !strings.HasPrefix(issued.Secret, "astral_") {
		t.Fatalf("issued shape: %+v", issued)
	}
	var row model.Credential
	if err := f.db.First(&row, "id = ?", issued.CredentialID).Error; err != nil {
		t.Fatal(err)
	}
	if row.WorkspaceID != nil {
		t.Fatalf("global credential must be unbound, got %v", *row.WorkspaceID)
	}

	// 吊销同样收口：user 403，admin 204。
	rec = f.callCredential(t, f.user, "DELETE", "/api/v1/agents/agt_cr1/credentials/"+issued.CredentialID, "")
	if rec.Code != 403 {
		t.Fatalf("user revoke status = %d", rec.Code)
	}
	rec = f.callCredential(t, f.admin, "DELETE", "/api/v1/agents/agt_cr1/credentials/"+issued.CredentialID, "")
	if rec.Code != 204 {
		t.Fatalf("admin revoke status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceBoundCredentialUnaffected(t *testing.T) {
	f := credSetup(t)

	// 普通用户不入 workspace：404（非成员不泄露存在性）。
	rec := f.callCredential(t, f.user, "POST", "/api/v1/agents/agt_cr1/credentials",
		`{"scopes":["task:read"],"workspace_id":"ws_nope"}`)
	if rec.Code != 404 {
		t.Fatalf("non-member bound issue status = %d", rec.Code)
	}

	// 把 user 拉进 workspace 后走 agent:manage 正常放行——收口只作用于全局路径。
	if err := f.db.Create(&model.Workspace{ID: "ws_cr", Name: "cr", Slug: "cr", CreatedBy: "usr_admin"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.WorkspaceMember{WorkspaceID: "ws_cr", ActorID: "usr_user", Role: "maintainer"}).Error; err != nil {
		t.Fatal(err)
	}
	rec = f.callCredential(t, f.user, "POST", "/api/v1/agents/agt_cr1/credentials",
		`{"scopes":["task:read"],"workspace_id":"ws_cr"}`)
	if rec.Code != 201 {
		t.Fatalf("maintainer bound issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
