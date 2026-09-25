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
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

type inviteFixture struct {
	m     *Module
	db    *gorm.DB
	wsID  string
	owner *auth.Principal
	agent *auth.Principal
}

func inviteSetup(t *testing.T) *inviteFixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(svc.DrainBackgroundWrites) // 先于关库/TempDir 清理 drain 后台写（cleanup LIFO）
	m := &Module{DB: db, Auth: svc, WebBaseURL: "https://app.example.com"}

	ownerActor := &model.Actor{ID: "usr_owner", Kind: "human", DisplayName: "Owner"}
	agentActor := &model.Actor{ID: "agt_t1", Kind: "agent", DisplayName: "T1"}
	for _, a := range []*model.Actor{ownerActor, agentActor} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	wsID := "ws_inv"
	if err := db.Create(&model.Workspace{ID: wsID, Name: "inv", Slug: "inv", CreatedBy: ownerActor.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, mem := range []model.WorkspaceMember{
		{WorkspaceID: wsID, ActorID: ownerActor.ID, Role: "owner"},
		{WorkspaceID: wsID, ActorID: agentActor.ID, Role: "agent"},
	} {
		if err := db.Create(&mem).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &inviteFixture{
		m: m, db: db, wsID: wsID,
		owner: &auth.Principal{ActorID: ownerActor.ID, Kind: "human"},
		agent: &auth.Principal{ActorID: agentActor.ID, Kind: "agent"},
	}
}

// callInvitation 按 chi 生产路由的等价方式分发邀请端点。
func (f *inviteFixture) callInvitation(t *testing.T, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	route := path
	if i := strings.IndexByte(route, '?'); i >= 0 {
		route = route[:i]
	}
	rctx := chi.NewRouteContext()
	if strings.Contains(route, "/invitations/") {
		i := strings.Index(route, "/invitations/")
		rest := route[i+len("/invitations/"):]
		rctx.URLParams.Add("invitation_id", strings.TrimSuffix(rest, "/revoke"))
	} else {
		rctx.URLParams.Add("workspace_id", f.wsID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithPrincipal(ctx, p)
	switch {
	case strings.HasSuffix(route, "/invitations") && method == "POST":
		f.m.createInvitation(rec, req.WithContext(ctx))
	case strings.HasSuffix(route, "/invitations") && method == "GET":
		f.m.listInvitations(rec, req.WithContext(ctx))
	case strings.HasSuffix(route, "/revoke"):
		f.m.revokeInvitation(rec, req.WithContext(ctx))
	default:
		t.Fatalf("unroutable path %q", path)
	}
	return rec
}

// issue 以 owner 签发一张邀请并返回 201 响应体。
func (f *inviteFixture) issue(t *testing.T, body string) map[string]any {
	t.Helper()
	rec := f.callInvitation(t, f.owner, "POST", "/api/v1/workspaces/"+f.wsID+"/invitations", body)
	if rec.Code != 201 {
		t.Fatalf("issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInvitationIssueListRevoke(t *testing.T) {
	f := inviteSetup(t)

	created := f.issue(t, `{"role":"contributor"}`)
	code, _ := created["code"].(string)
	if code == "" {
		t.Fatalf("code missing: %v", created)
	}
	// 形状：XXXXX-XXXXX-XXXXX-XXXXX；库中只有 hash，明文不落库。
	if len(strings.Split(code, "-")) != 4 {
		t.Fatalf("code grouping: %q", code)
	}
	var row model.Invitation
	if err := f.db.First(&row, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.CodeHash, code) {
		t.Fatal("hash must not contain plaintext")
	}
	inviteURL, _ := created["invite_url"].(string)
	if inviteURL != "https://app.example.com/register?code="+code {
		t.Fatalf("invite_url = %q", inviteURL)
	}
	// TTL 缺省 7d。
	if row.ExpiresAt.Sub(row.CreatedAt) != 7*24*3600*1e9 {
		t.Fatalf("default TTL = %v", row.ExpiresAt.Sub(row.CreatedAt))
	}

	// 列表：status=invited 命中；列表项不含 code。
	rec := f.callInvitation(t, f.owner, "GET", "/api/v1/workspaces/"+f.wsID+"/invitations?status=invited", "")
	if rec.Code != 200 {
		t.Fatalf("list status = %d", rec.Code)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("list items = %d", len(page.Items))
	}
	if _, ok := page.Items[0]["code"]; ok {
		t.Fatal("list must not expose code")
	}

	// 撤销 + 幂等。
	invID, _ := created["id"].(string)
	for i := 0; i < 2; i++ {
		rec := f.callInvitation(t, f.owner, "POST", "/api/v1/invitations/"+invID+"/revoke", "")
		if rec.Code != 204 {
			t.Fatalf("revoke #%d status = %d", i, rec.Code)
		}
	}
	if err := f.db.First(&row, "id = ?", invID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "revoked" {
		t.Fatalf("status = %q", row.Status)
	}
	// 事件：created + revoked 各一条。
	var n int64
	if err := f.db.Model(&model.OutboxEvent{}).Where("type IN ?", []string{
		outbox.TypeSecurityInviteCreated, outbox.TypeSecurityInviteRevoked,
	}).Count(&n).Error; err != nil || n != 2 {
		t.Fatalf("invite events = %d (err=%v)", n, err)
	}

	// 未知邀请 404。
	rec = f.callInvitation(t, f.owner, "POST", "/api/v1/invitations/inv_nope/revoke", "")
	if rec.Code != 404 {
		t.Fatalf("unknown invitation status = %d", rec.Code)
	}
}

func TestInvitationAuthorizationAndValidation(t *testing.T) {
	f := inviteSetup(t)

	// agent credential 一律 403（签发/列表/撤销同语义）。
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/workspaces/" + f.wsID + "/invitations"},
		{"GET", "/api/v1/workspaces/" + f.wsID + "/invitations"},
		{"POST", "/api/v1/invitations/inv_x/revoke"},
	} {
		rec := f.callInvitation(t, f.agent, tc.method, tc.path, `{"role":"viewer"}`)
		if rec.Code != 403 {
			t.Fatalf("%s %s as agent: status = %d", tc.method, tc.path, rec.Code)
		}
	}

	// owner 永不经邀请；非法 expires_in 拒绝。
	for _, body := range []string{
		`{"role":"owner"}`,
		`{"role":"agent"}`,
		`{"role":"viewer","expires_in":0}`,
		`{"role":"viewer","expires_in":2592001}`,
	} {
		rec := f.callInvitation(t, f.owner, "POST", "/api/v1/workspaces/"+f.wsID+"/invitations", body)
		if rec.Code != 400 {
			t.Fatalf("body %s: status = %d", body, rec.Code)
		}
	}

	// 非法 status 过滤 400。
	rec := f.callInvitation(t, f.owner, "GET", "/api/v1/workspaces/"+f.wsID+"/invitations?status=weird", "")
	if rec.Code != 400 {
		t.Fatalf("bad filter status = %d", rec.Code)
	}

	// expires_in 显式生效。
	created := f.issue(t, `{"role":"viewer","expires_in":3600}`)
	var row model.Invitation
	if err := f.db.First(&row, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt.Sub(row.CreatedAt) != 3600e9 {
		t.Fatalf("ttl = %v", row.ExpiresAt.Sub(row.CreatedAt))
	}
}
