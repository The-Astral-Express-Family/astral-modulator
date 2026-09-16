// audit_test.go 是审计查询双端点的 app 级 HTTP 集成测试（round 38 T5 /
// TODO.md S4-1/S4-2）：GET /workspaces/{id}/audit 与 GET /admin/audit 的
// 过滤组合、id 游标翻页、空集空信封、鉴权语义（非成员 404 / scope 不足
// 403 / 未认证 401 / platform:audit:read 不足 403）、平台端点必含
// workspace_id IS NULL 的服务器级记录。
package tests

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/app"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/admin"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/document"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/memory"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/message"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/presence"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/task"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/workspace"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// 审计测试的固定主体与工作区（ID 不必符合 wire pattern：服务端只做字符串
// 等值过滤，不校验形状）。
const (
	auWSA    = "ws_au_a"
	auWSB    = "ws_au_b"       // owner 也是成员；有一行审计行（跨 workspace 可见性）
	auWSC    = "ws_au_c"       // owner 也是成员、无审计行 → 空集用
	auAdmin  = "usr_au_admin"  // platform_role=admin
	auPlain  = "usr_au_plain"  // platform_role=user（platform:audit:read 不足）
	auOwner  = "usr_au_owner"  // wsA/wsB/wsC owner（audit:read 经 AllScopes）
	auViewer = "usr_au_viewer" // wsA viewer（bundle 无 audit:read）→ 403
	auOut    = "usr_au_out"    // 无 membership → 404
	auActA   = "usr_au_a"      // 审计行主角（跨 workspace/服务器级出现）
	auActB   = "usr_au_b"
)

// auditFixture 起完整 app（sqlite），预置 human 主体 + 会话 + 双 workspace
// membership，并经真实写入管线（GormRecorder，含 redaction）落审计行。
type auditFixture struct {
	ts     *httptest.Server
	db     *gorm.DB
	base   string
	owner  string // 各 human 的 access token
	viewer string
	out    string
	admin  string
	plain  string
}

// humanWithSession 落一个 human actor + 手工会话行（authenticateAccessToken
// 的最小等价物：access hash + 未过期 + actor 未停用），返回 Bearer token。
// 不走 bcrypt 登录管道：本文件测查询端点，认证管线已由 auth 包测试覆盖。
func humanWithSession(t *testing.T, db *gorm.DB, id, platformRole string) string {
	t.Helper()
	if err := db.Create(&model.Actor{ID: id, Kind: "human", PlatformRole: platformRole, DisplayName: id}).Error; err != nil {
		t.Fatal(err)
	}
	token := "ata_test_" + id
	sess := &model.Session{
		ID: ids.New(ids.Session), ActorID: id, ClientType: "web",
		RefreshTokenHash: auth.HashToken("atr_test_" + id),
		FamilyID:         ids.New(ids.Session),
		AccessTokenHash:  auth.HashToken(token),
		AccessExpiresAt:  time.Now().Add(15 * time.Minute),
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	if err := db.Create(sess).Error; err != nil {
		t.Fatal(err)
	}
	return token
}

// auRecord 经 GormRecorder 落一条审计行（真实写入管线：redaction +
// request_id 解析），返回供断言的关键字段。
func auRecord(t *testing.T, db *gorm.DB, e audit.Entry) {
	t.Helper()
	if err := (&audit.GormRecorder{DB: db}).Record(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

// seedAuditRows 铺全文件共用的数据面：wsA 四行（覆盖 actor/action/outcome
// 组合与 redaction）、wsB 一行、服务器级（workspace_id IS NULL）两行。
func seedAuditRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	auRecord(t, db, audit.Entry{WorkspaceID: auWSA, ActorID: auActA,
		Action: "task.claim", Outcome: "allowed",
		TargetType: "task", TargetID: "tsk_au_1",
		Details: map[string]any{"note": "visible"}, RequestID: "req_au_1"})
	auRecord(t, db, audit.Entry{WorkspaceID: auWSA, ActorID: auActB,
		Action: "task.claim", Outcome: "denied",
		TargetType: "task", TargetID: "tsk_au_1"})
	auRecord(t, db, audit.Entry{WorkspaceID: auWSA, ActorID: auActA,
		Action: "document.push", Outcome: "allowed", TargetType: "document"})
	auRecord(t, db, audit.Entry{WorkspaceID: auWSA, ActorID: auActA,
		Action: "auth.login", Outcome: "denied",
		Details: map[string]any{"password": "hunter2", "note": "kept"}})
	auRecord(t, db, audit.Entry{WorkspaceID: auWSB, ActorID: auActA,
		Action: "task.claim", Outcome: "allowed"})
	// 服务器级：无 workspace、actor 一有一无（S4-3 落地前的形状预演）。
	auRecord(t, db, audit.Entry{ActorID: auActA, Action: "auth.bearer", Outcome: "denied"})
	auRecord(t, db, audit.Entry{Action: "auth.login", Outcome: "denied"})
}

func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := testsupport.NewTestDB(t)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	svc := auth.NewService(db, log)
	hub := event.NewHub()
	mods := &app.Modules{
		Idempotency: nil,
		Auth:        &auth.Module{Svc: svc, PublicURL: "https://astral.example.com"},
		Workspace:   &workspace.Module{DB: db, Auth: svc},
		Task:        &task.Module{DB: db, Auth: svc},
		Tag:         &tag.Module{Auth: svc},
		Memory:      &memory.Module{},
		Document:    &document.Module{DB: db, Auth: svc},
		Message:     &message.Module{DB: db, Auth: svc},
		Presence:    &presence.Module{DB: db, Auth: svc},
		Audit: &audit.Module{DB: db, Authorize: func(r *http.Request, workspaceID string) *httpx.APIError {
			return auth.RequireWorkspace(r, svc, workspaceID, auth.ScopeAuditRead)
		}},
		Admin:  &admin.Module{DB: db, Auth: svc, Log: log},
		Events: &event.SSEHandler{Hub: hub, DB: db, Auth: svc},
	}
	mods.Message.Tasks = mods.Task
	ts := httptest.NewServer(app.NewRouter(config.Config{ServerID: "srv_autest", PublicURL: "https://astral.example.com"}, log, db, mods))
	t.Cleanup(ts.Close)

	// 主体与 membership。
	for _, m := range []model.WorkspaceMember{
		{WorkspaceID: auWSA, ActorID: auOwner, Role: "owner"},
		{WorkspaceID: auWSA, ActorID: auViewer, Role: "viewer"},
		{WorkspaceID: auWSB, ActorID: auOwner, Role: "owner"},
		{WorkspaceID: auWSC, ActorID: auOwner, Role: "owner"},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedAuditRows(t, db)

	return &auditFixture{
		ts:     ts,
		db:     db,
		base:   ts.URL + "/api/v1",
		owner:  humanWithSession(t, db, auOwner, "user"),
		viewer: humanWithSession(t, db, auViewer, "user"),
		out:    humanWithSession(t, db, auOut, "user"),
		admin:  humanWithSession(t, db, auAdmin, "admin"),
		plain:  humanWithSession(t, db, auPlain, "user"),
	}
}

// auGet 发带 query 的 GET 并返回 (status, body)。
func auGet(t *testing.T, f *auditFixture, path, token, query string) (int, map[string]any) {
	t.Helper()
	url := f.base + path
	if query != "" {
		url += "?" + query
	}
	code, body, _ := docDo(t, http.MethodGet, url, token, nil, nil)
	return code, body
}

// auItems 取分页信封的 items；断言其为数组。
func auItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("missing items array: %v", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("item is not an object: %v", it)
		}
		out = append(out, m)
	}
	return out
}

// auActions 按序取 items 的 action（顺序断言用）。
func auActions(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it["action"].(string))
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- workspace 端点（S4-1）----

func TestWorkspaceAuditListAndFilters(t *testing.T) {
	f := newAuditFixture(t)

	// 无过滤：wsA 四行，id 降序（uuidv7 字典序 = 写入序，最晚写入在最前）。
	code, body := auGet(t, f, "/workspaces/"+auWSA+"/audit", f.owner, "")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d %v", code, body)
	}
	items := auItems(t, body)
	if len(items) != 4 {
		t.Fatalf("want 4 items, got %d: %v", len(items), auActions(items))
	}
	wantDesc := []string{"auth.login", "document.push", "task.claim", "task.claim"}
	if got := auActions(items); !eqStrings(got, wantDesc) {
		t.Fatalf("want newest-first %v, got %v", wantDesc, got)
	}
	// 行形状：workspace 收口 + 可空字段省略 + details redaction。
	for _, it := range items {
		if it["workspace_id"] != auWSA {
			t.Fatalf("leaked non-workspace row: %v", it)
		}
		if it["created_at"] == nil || it["outcome"] == nil {
			t.Fatalf("missing required fields: %v", it)
		}
	}
	// 最晚写入的 auth.login 行：actor/request_id（省略）/details redaction。
	latest := items[0]
	if latest["actor_id"] != auActA {
		t.Fatalf("want actor %s, got %v", auActA, latest["actor_id"])
	}
	if _, has := latest["request_id"]; has {
		t.Fatalf("request_id should be omitted when NULL: %v", latest)
	}
	details, ok := latest["details"].(map[string]any)
	if !ok {
		t.Fatalf("details should be an object: %v", latest["details"])
	}
	if details["password"] != "[REDACTED]" || details["note"] != "kept" {
		t.Fatalf("redaction broken: %v", details)
	}
	// 最早写入的 task.claim 行：request_id 回显 + target_type/target_id。
	oldest := items[3]
	if oldest["request_id"] != "req_au_1" || oldest["target_type"] != "task" || oldest["target_id"] != "tsk_au_1" {
		t.Fatalf("first row shape mismatch: %v", oldest)
	}

	cases := []struct {
		query string
		want  []string // action 序列（降序）
	}{
		{"action=task.claim", []string{"task.claim", "task.claim"}},
		{"outcome=denied", []string{"auth.login", "task.claim"}},
		{"actor_id=" + auActB, []string{"task.claim"}},
		{"action=task.claim&outcome=allowed", []string{"task.claim"}},
		{"actor_id=" + auActA + "&outcome=denied", []string{"auth.login"}},
		// 过滤不到行 = 空信封（items [] + next_cursor null）。
		{"action=workspace.create", []string{}},
	}
	for _, tc := range cases {
		code, body := auGet(t, f, "/workspaces/"+auWSA+"/audit", f.owner, tc.query)
		if code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d %v", tc.query, code, body)
		}
		items := auItems(t, body)
		if got := auActions(items); !eqStrings(got, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.query, tc.want, got)
		}
		if body["next_cursor"] != nil {
			t.Fatalf("%s: next_cursor should be null, got %v", tc.query, body["next_cursor"])
		}
	}

	// outcome 非法 → 400 VALIDATION_FAILED（details.field=outcome）。
	code, body = auGet(t, f, "/workspaces/"+auWSA+"/audit", f.owner, "outcome=maybe")
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %v", code, body)
	}
	if c := errCode(t, body); c != "VALIDATION_FAILED" {
		t.Fatalf("want VALIDATION_FAILED, got %s", c)
	}
	if errDetails(t, body)["field"] != "outcome" {
		t.Fatalf("want details.field=outcome, got %v", body)
	}
}

func TestWorkspaceAuditCursor(t *testing.T) {
	f := newAuditFixture(t)

	// limit=2 翻完 wsA 四行：顺序连续、无重复，末页 next_cursor=null。
	var got []string
	cursor := ""
	pages := 0
	for {
		query := "limit=2"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		code, body := auGet(t, f, "/workspaces/"+auWSA+"/audit", f.owner, query)
		if code != http.StatusOK {
			t.Fatalf("page %d: want 200, got %d %v", pages, code, body)
		}
		items := auItems(t, body)
		got = append(got, auActions(items)...)
		pages++
		next, _ := body["next_cursor"].(string)
		if next == "" {
			if body["next_cursor"] != nil {
				t.Fatalf("next_cursor should be null at end, got %v", body["next_cursor"])
			}
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if pages != 2 {
		t.Fatalf("want 2 pages, got %d", pages)
	}
	want := []string{"auth.login", "document.push", "task.claim", "task.claim"}
	if !eqStrings(got, want) {
		t.Fatalf("paged actions %v, want %v", got, want)
	}
}

func TestWorkspaceAuditEmptyEnvelope(t *testing.T) {
	f := newAuditFixture(t)

	// wsC 有 membership、无审计行：空集也必须是空数组信封而非 null。
	code, body := auGet(t, f, "/workspaces/"+auWSC+"/audit", f.owner, "")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d %v", code, body)
	}
	if items := auItems(t, body); len(items) != 0 {
		t.Fatalf("want empty items, got %v", items)
	}
	if body["next_cursor"] != nil {
		t.Fatalf("want null next_cursor, got %v", body["next_cursor"])
	}
}

func TestWorkspaceAuditAuthorization(t *testing.T) {
	f := newAuditFixture(t)

	// viewer 成员但 bundle 无 audit:read → 403 INSUFFICIENT_SCOPE。
	code, body := auGet(t, f, "/workspaces/"+auWSA+"/audit", f.viewer, "")
	if code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %v", code, body)
	}
	if c := errCode(t, body); c != "INSUFFICIENT_SCOPE" {
		t.Fatalf("want INSUFFICIENT_SCOPE, got %s", c)
	}
	// 非成员 → 404 WORKSPACE_NOT_FOUND（不泄露存在性）。
	code, body = auGet(t, f, "/workspaces/"+auWSA+"/audit", f.out, "")
	if code != http.StatusNotFound {
		t.Fatalf("want 404, got %d %v", code, body)
	}
	if c := errCode(t, body); c != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("want WORKSPACE_NOT_FOUND, got %s", c)
	}
	// 未认证 → 401。
	code, body = auGet(t, f, "/workspaces/"+auWSA+"/audit", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %v", code, body)
	}
}

// ---- 平台端点（S4-2）----

func TestAdminAuditListAndFilters(t *testing.T) {
	f := newAuditFixture(t)

	// 无过滤：全部 7 行，含 workspace_id IS NULL 的服务器级记录。
	code, body := auGet(t, f, "/admin/audit", f.admin, "")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d %v", code, body)
	}
	items := auItems(t, body)
	if len(items) != 7 {
		t.Fatalf("want 7 items, got %d: %v", len(items), auActions(items))
	}
	serverLevel := 0
	for _, it := range items {
		if _, has := it["workspace_id"]; !has {
			serverLevel++
		}
	}
	if serverLevel != 2 {
		t.Fatalf("want 2 server-level (workspace_id NULL) rows, got %d", serverLevel)
	}
	// 服务器级行在降序流最前（最后写入）。
	if got := auActions(items[:2]); !eqStrings(got, []string{"auth.login", "auth.bearer"}) {
		t.Fatalf("server-level rows should lead the timeline, got %v", auActions(items))
	}

	cases := []struct {
		query       string
		wantActions []string
		wantNullWS  int // workspace_id 省略（服务器级）行数
	}{
		{"workspace_id=" + auWSA, []string{"auth.login", "document.push", "task.claim", "task.claim"}, 0},
		{"workspace_id=" + auWSB, []string{"task.claim"}, 0},
		// actor=auActA：wsA 3 行 + wsB 1 行 + 服务器级 1 行（跨工作区 + NULL 混合）。
		{"actor_id=" + auActA, []string{"auth.bearer", "task.claim", "auth.login", "document.push", "task.claim"}, 1},
		{"action=auth.login", []string{"auth.login", "auth.login"}, 1},
		{"outcome=denied", []string{"auth.login", "auth.bearer", "auth.login", "task.claim"}, 2},
		// workspace_id 过滤到无行 workspace：空信封。
		{"workspace_id=ws_au_none", []string{}, 0},
	}
	for _, tc := range cases {
		code, body := auGet(t, f, "/admin/audit", f.admin, tc.query)
		if code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d %v", tc.query, code, body)
		}
		items := auItems(t, body)
		if got := auActions(items); !eqStrings(got, tc.wantActions) {
			t.Fatalf("%s: want actions %v, got %v", tc.query, tc.wantActions, got)
		}
		nullWS := 0
		for _, it := range items {
			if _, has := it["workspace_id"]; !has {
				nullWS++
			}
		}
		if nullWS != tc.wantNullWS {
			t.Fatalf("%s: want %d server-level rows, got %d", tc.query, tc.wantNullWS, nullWS)
		}
	}

	// outcome 非法 → 400（与 workspace 端点同口径）。
	code, body = auGet(t, f, "/admin/audit", f.admin, "outcome=maybe")
	if code != http.StatusBadRequest || errCode(t, body) != "VALIDATION_FAILED" {
		t.Fatalf("want 400 VALIDATION_FAILED, got %d %v", code, body)
	}
}

func TestAdminAuditCursor(t *testing.T) {
	f := newAuditFixture(t)

	// limit=3 翻完全部 7 行（服务器级行在第 1 页——平台时间线最顶部）。
	var got []string
	cursor := ""
	pages := 0
	for {
		query := "limit=3"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		code, body := auGet(t, f, "/admin/audit", f.admin, query)
		if code != http.StatusOK {
			t.Fatalf("page %d: want 200, got %d %v", pages, code, body)
		}
		got = append(got, auActions(auItems(t, body))...)
		pages++
		next, _ := body["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	want := []string{
		"auth.login", "auth.bearer", // 服务器级
		"task.claim",                                              // wsB
		"auth.login", "document.push", "task.claim", "task.claim", // wsA
	}
	if !eqStrings(got, want) {
		t.Fatalf("paged actions %v, want %v", got, want)
	}
}

func TestAdminAuditForbidden(t *testing.T) {
	f := newAuditFixture(t)

	// platform_role=user（platform:audit:read 不足）→ 403 INSUFFICIENT_SCOPE。
	code, body := auGet(t, f, "/admin/audit", f.plain, "")
	if code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %v", code, body)
	}
	if c := errCode(t, body); c != "INSUFFICIENT_SCOPE" {
		t.Fatalf("want INSUFFICIENT_SCOPE, got %s", c)
	}
	// workspace owner 也无平台特权（workspace 轴 ≠ 平台轴）。
	code, body = auGet(t, f, "/admin/audit", f.owner, "")
	if code != http.StatusForbidden {
		t.Fatalf("want 403 for workspace owner, got %d %v", code, body)
	}
	// 未认证 → 401。
	code, body = auGet(t, f, "/admin/audit", "", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %v", code, body)
	}
}
