package document

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// scopeFixture 准备一个 workspace 与三名人成员（viewer/contributor/owner，
// RoleToScopes 实际 bundle），以及 workspace 外的一名 human。
type scopeFixture struct {
	svc  *auth.Service
	wsID string
}

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	wsID := "ws_scope"
	if err := db.Create(&model.Workspace{ID: wsID, Name: "scope", Slug: "scope", CreatedBy: "usr_owner"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ actorID, role string }{
		{"usr_viewer", "viewer"},
		{"usr_contrib", "contributor"},
		{"usr_owner", "owner"},
		{"usr_outsider", ""},
	} {
		if err := db.Create(&model.Actor{ID: m.actorID, Kind: "human", DisplayName: m.actorID}).Error; err != nil {
			t.Fatal(err)
		}
		if m.role == "" {
			continue // 非成员
		}
		if err := db.Create(&model.WorkspaceMember{WorkspaceID: wsID, ActorID: m.actorID, Role: m.role}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &scopeFixture{svc: svc, wsID: wsID}
}

func principalRequest(p *auth.Principal) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws_scope/documents/memory/a.md", nil)
	return req.WithContext(auth.WithPrincipal(req.Context(), p))
}

func humanPrincipal(actorID string) *auth.Principal {
	return &auth.Principal{ActorID: actorID, Kind: "human"}
}

// credentialPrincipal 构造 credential 认证的 agent 主体（WorkspaceScopes 直接
// 读 credential scopes，不经成员角色）。
func credentialPrincipal(actorID, wsID string, scopes string) *auth.Principal {
	ws := wsID
	return &auth.Principal{
		ActorID: actorID, Kind: "agent", AuthKind: "credential",
		Credential: &model.Credential{WorkspaceID: &ws, Scopes: scopes},
	}
}

// TestRequireDocScopePrefixAxes 断言前缀 × 读写的 scope 选择两轴：
// memory/** ↔ memory:read/write，其余路径 ↔ document:read/write。
// 用例按 RoleToScopes 实际 bundle 设计：viewer = 两个 read、无 write；
// contributor/agent = 全套；memory-only credential 只能碰 memory/**。
func TestRequireDocScopePrefixAxes(t *testing.T) {
	f := newScopeFixture(t)

	cases := []struct {
		name      string
		principal *auth.Principal
		path      string
		read      bool
		wantErr   string // "" = 通过；否则为期望的 error.code
	}{
		// viewer（MemoryRead+DocumentRead，无任何 write）。
		{"viewer read memory", humanPrincipal("usr_viewer"), "memory/project.md", true, ""},
		{"viewer read notes", humanPrincipal("usr_viewer"), "notes/a.md", true, ""},
		{"viewer write memory denied", humanPrincipal("usr_viewer"), "memory/project.md", false, httpx.CodeInsufficientScope},
		{"viewer write notes denied", humanPrincipal("usr_viewer"), "notes/a.md", false, httpx.CodeInsufficientScope},
		// contributor（memory+document 全套）。
		{"contributor read memory", humanPrincipal("usr_contrib"), "memory/project.md", true, ""},
		{"contributor write memory", humanPrincipal("usr_contrib"), "memory/project.md", false, ""},
		{"contributor read notes", humanPrincipal("usr_contrib"), "notes/a.md", true, ""},
		{"contributor write notes", humanPrincipal("usr_contrib"), "notes/a.md", false, ""},
		// memory-only credential：memory/** 读写皆可，非 memory 路径全拒。
		{"memory credential read memory", credentialPrincipal("agt_m", f.wsID, `["memory:read","memory:write"]`), "memory/x.md", true, ""},
		{"memory credential write memory", credentialPrincipal("agt_m", f.wsID, `["memory:read","memory:write"]`), "memory/x.md", false, ""},
		{"memory credential read notes denied", credentialPrincipal("agt_m", f.wsID, `["memory:read","memory:write"]`), "notes/a.md", true, httpx.CodeInsufficientScope},
		{"memory credential write notes denied", credentialPrincipal("agt_m", f.wsID, `["memory:read","memory:write"]`), "notes/a.md", false, httpx.CodeInsufficientScope},
		// document-only credential：非 memory 路径可读写，memory/** 全拒。
		{"document credential read notes", credentialPrincipal("agt_d", f.wsID, `["document:read","document:write"]`), "notes/a.md", true, ""},
		{"document credential write notes", credentialPrincipal("agt_d", f.wsID, `["document:read","document:write"]`), "notes/a.md", false, ""},
		{"document credential read memory denied", credentialPrincipal("agt_d", f.wsID, `["document:read","document:write"]`), "memory/x.md", true, httpx.CodeInsufficientScope},
		{"document credential write memory denied", credentialPrincipal("agt_d", f.wsID, `["document:read","document:write"]`), "memory/x.md", false, httpx.CodeInsufficientScope},
		// 前缀判断区分大小写：Memory/ 走 document 轴。
		{"case-sensitive prefix document axis", credentialPrincipal("agt_m", f.wsID, `["memory:read","memory:write"]`), "Memory/x.md", true, httpx.CodeInsufficientScope},
		{"bare memory filename is document axis", credentialPrincipal("agt_d", f.wsID, `["document:read"]`), "memory", true, ""},
		// 非成员：404 不泄露存在性（两轴一致）。
		{"outsider read memory", humanPrincipal("usr_outsider"), "memory/x.md", true, httpx.CodeWorkspaceNotFound},
		{"outsider read notes", humanPrincipal("usr_outsider"), "notes/a.md", true, httpx.CodeWorkspaceNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, apiErr := requireDocScope(principalRequest(tc.principal), f.svc, f.wsID, tc.path, tc.read)
			if tc.wantErr == "" {
				if apiErr != nil {
					t.Fatalf("want pass, got %d %s", apiErr.Status, apiErr.Code)
				}
				if p == nil || p.ActorID != tc.principal.ActorID {
					t.Fatalf("principal not returned: %+v", p)
				}
				return
			}
			if apiErr == nil {
				t.Fatal("want error, got pass")
			}
			if apiErr.Code != tc.wantErr {
				t.Fatalf("code = %s (%d), want %s", apiErr.Code, apiErr.Status, tc.wantErr)
			}
			if tc.wantErr == httpx.CodeInsufficientScope && apiErr.Status != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", apiErr.Status)
			}
			if tc.wantErr == httpx.CodeWorkspaceNotFound && apiErr.Status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", apiErr.Status)
			}
		})
	}
}

// TestScopeFor 是 scopeFor 的纯函数快照表（防前缀轴回归）。
func TestScopeFor(t *testing.T) {
	cases := []struct {
		path string
		read bool
		want string
	}{
		{"memory/a.md", true, auth.ScopeMemoryRead},
		{"memory/a.md", false, auth.ScopeMemoryWrite},
		{"memory/deep/nested/a.md", true, auth.ScopeMemoryRead},
		{"notes/a.md", true, auth.ScopeDocumentRead},
		{"notes/a.md", false, auth.ScopeDocumentWrite},
		{"Memory/a.md", true, auth.ScopeDocumentRead},
		{"memory", true, auth.ScopeDocumentRead},
		{"memories/a.md", true, auth.ScopeDocumentRead},
	}
	for _, tc := range cases {
		if got := scopeFor(tc.path, tc.read); got != tc.want {
			t.Errorf("scopeFor(%q, %v) = %s, want %s", tc.path, tc.read, got, tc.want)
		}
	}
}
