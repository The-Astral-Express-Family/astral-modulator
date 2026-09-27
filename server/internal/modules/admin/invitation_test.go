// 平台级注册邀请（00018）：签发/列表/撤销管理面测试。兑换路径在 auth
// （platform_invite_test.go）。授权矩阵对齐 module_test.go：user/agent
// 一律 403，仅平台 admin（platform:users:manage）可操作。
package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// callInvitation 以注入 principal 直调 handler（沿用 module_test.go 惯例）。
func (f *fixture) callInvitation(t *testing.T, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	bare := strings.SplitN(strings.TrimPrefix(path, "/api/v1/admin/invitations"), "?", 2)[0]
	if bare != "" && bare != "/" {
		rctx.URLParams.Add("invitation_id", strings.Split(strings.Trim(bare, "/"), "/")[0])
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	switch {
	case bare == "" && method == "POST":
		f.m.createPlatformInvitation(rec, req)
	case bare == "" && method == "GET":
		f.m.listPlatformInvitations(rec, req)
	case strings.HasSuffix(bare, "/revoke") && method == "POST":
		f.m.revokePlatformInvitation(rec, req)
	default:
		t.Fatalf("unroutable %s %q", method, path)
	}
	return rec
}

func (f *fixture) issuePlatform(t *testing.T, body string) map[string]any {
	t.Helper()
	rec := f.callInvitation(t, f.admin, "POST", "/api/v1/admin/invitations", body)
	if rec.Code != 201 {
		t.Fatalf("issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPlatformInvitationIssueListRevoke(t *testing.T) {
	f := newFixture(t)

	created := f.issuePlatform(t, `{}`)
	code, _ := created["code"].(string)
	if code == "" || len(strings.Split(code, "-")) != 4 {
		t.Fatalf("code shape: %q", code)
	}
	var row model.PlatformInvitation
	if err := f.db.First(&row, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.CodeHash, code) {
		t.Fatal("hash must not contain plaintext")
	}
	// 默认 TTL 7d。
	if row.ExpiresAt.Sub(row.CreatedAt) != 7*24*3600*time.Second {
		t.Fatalf("default TTL = %v", row.ExpiresAt.Sub(row.CreatedAt))
	}
	// workspace 邀请表不受污染。
	var wsCount int64
	if err := f.db.Model(&model.Invitation{}).Count(&wsCount).Error; err != nil || wsCount != 0 {
		t.Fatalf("workspace invitations polluted: %d (err=%v)", wsCount, err)
	}

	// 列表：status=invited 命中；不回传 code。
	rec := f.callInvitation(t, f.admin, "GET", "/api/v1/admin/invitations?status=invited", "")
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
	if _, ok := page.Items[0]["workspace_id"]; ok {
		t.Fatal("platform invitation must not carry workspace_id")
	}

	// 撤销幂等 + 状态落库。
	invID, _ := created["id"].(string)
	for i := 0; i < 2; i++ {
		rec := f.callInvitation(t, f.admin, "POST", "/api/v1/admin/invitations/"+invID+"/revoke", "")
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
	// 事件：created + revoked 各一条（scope=platform）。
	var events []model.OutboxEvent
	if err := f.db.Where("type IN ?", []string{
		outbox.TypeSecurityInviteCreated, outbox.TypeSecurityInviteRevoked,
	}).Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatalf("invite events = %d (err=%v)", len(events), err)
	}
	for _, e := range events {
		if e.WorkspaceID != nil {
			t.Fatalf("platform invite event must not carry workspace: %v", e.WorkspaceID)
		}
	}

	// 未知邀请 404；非法 status 过滤 400。
	if rec := f.callInvitation(t, f.admin, "POST", "/api/v1/admin/invitations/inv_nope/revoke", ""); rec.Code != 404 {
		t.Fatalf("unknown invitation status = %d", rec.Code)
	}
	if rec := f.callInvitation(t, f.admin, "GET", "/api/v1/admin/invitations?status=weird", ""); rec.Code != 400 {
		t.Fatalf("bad filter status = %d", rec.Code)
	}
}

func TestPlatformInvitationAuthorizationAndValidation(t *testing.T) {
	f := newFixture(t)

	// user / agent 一律 403（与 /admin/users* 同矩阵）。
	for _, tc := range []struct {
		p      *auth.Principal
		method string
		path   string
	}{
		{f.user, "POST", "/api/v1/admin/invitations"},
		{f.user, "GET", "/api/v1/admin/invitations"},
		{f.user, "POST", "/api/v1/admin/invitations/inv_x/revoke"},
		{f.agentP, "POST", "/api/v1/admin/invitations"},
	} {
		rec := f.callInvitation(t, tc.p, tc.method, tc.path, `{}`)
		if rec.Code != 403 {
			t.Fatalf("%s %s as %s: status = %d", tc.method, tc.path, tc.p.PlatformRole, rec.Code)
		}
	}

	// 非法 expires_in 拒绝；显式 expires_in 生效。
	for _, body := range []string{`{"expires_in":0}`, `{"expires_in":2592001}`} {
		rec := f.callInvitation(t, f.admin, "POST", "/api/v1/admin/invitations", body)
		if rec.Code != 400 {
			t.Fatalf("body %s: status = %d", body, rec.Code)
		}
	}
	created := f.issuePlatform(t, `{"expires_in":3600}`)
	var row model.PlatformInvitation
	if err := f.db.First(&row, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt.Sub(row.CreatedAt) != time.Hour {
		t.Fatalf("ttl = %v", row.ExpiresAt.Sub(row.CreatedAt))
	}
	// invite_url 形状：基址来自请求 Host 回退链。
	if u, _ := created["invite_url"].(string); !strings.HasSuffix(u, "/register?code="+code(created)) {
		t.Fatalf("invite_url = %q", u)
	}
}

func code(created map[string]any) string { return created["code"].(string) }
