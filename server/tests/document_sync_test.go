// document_sync_test.go 是 document 模块七端点的 app 级 HTTP 集成测试
// （round 38 T4）：覆盖 docs/sync-semantics.md §19 前八项的服务端等价物
// （local-only / remote-only / dual / edit-vs-delete / delete-vs-edit /
// 重复删幂等 / tombstone 复活 / hash 不符）、R1 大小写冲突、scope 前缀两轴、
// manifest 游标与 include_deleted、1MiB 上限、resolve 四分支与幂等重放。
package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/app"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/idempotency"
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

// docFixture 起一个完整 app（sqlite），预置一个 workspace 与三类 credential：
// fullA/fullB（document+memory 全套，两名 agent 模拟双端）、readOnly（两轴只读）、
// memOnly（仅 memory 轴）。
type docFixture struct {
	ts       *httptest.Server
	db       *gorm.DB
	base     string
	wsID     string
	fullA    string
	fullB    string
	readOnly string
	memOnly  string
}

func newDocFixture(t *testing.T) *docFixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := testsupport.NewTestDB(t)
	// 单连接收口：credential 认证会异步 UPDATE last_used_at（auth/service.go
	// authenticateCredential 的 go func），glebarez sqlite 无 busy_timeout 时，
	// 该后台写在另一条池连接上与本测试的业务事务抢写锁 → SQLITE_BUSY。
	// 收敛到单连接后所有语句串行（异步写排队等待），确定性消除竞争。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	svc := auth.NewService(db, log)
	hub := event.NewHub()
	mods := &app.Modules{
		Idempotency: &idempotency.Middleware{DB: db, Log: log},
		Auth:        &auth.Module{Svc: svc, PublicURL: "https://astral.example.com"},
		Workspace:   &workspace.Module{DB: db, Auth: svc},
		Task:        &task.Module{DB: db, Auth: svc},
		Tag:         &tag.Module{},
		Memory:      &memory.Module{},
		Document:    &document.Module{DB: db, Auth: svc},
		Message:     &message.Module{DB: db, Auth: svc},
		Presence:    &presence.Module{DB: db, Auth: svc},
		Audit:       &audit.Module{},
		Admin:       &admin.Module{DB: db, Auth: svc, Log: log},
		Events:      &event.SSEHandler{Hub: hub, DB: db, Auth: svc},
	}
	mods.Message.Tasks = mods.Task
	ts := httptest.NewServer(app.NewRouter(config.Config{ServerID: "srv_doctest", PublicURL: "https://astral.example.com"}, log, db, mods))
	t.Cleanup(ts.Close)

	wsID := "ws_doctest"
	human := &model.Actor{ID: "usr_doc_h", Kind: "human", DisplayName: "Doc Human"}
	if err := db.Create(human).Error; err != nil {
		t.Fatal(err)
	}
	// credential 认证每请求回查 actors 行（authActor），agent 主体必须先落库。
	for _, id := range []string{"agt_doc_a", "agt_doc_b", "agt_doc_ro", "agt_doc_mem"} {
		if err := db.Create(&model.Actor{ID: id, Kind: "agent", DisplayName: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Workspace{ID: wsID, Name: "doctest", Slug: "doctest", CreatedBy: human.ID}).Error; err != nil {
		t.Fatal(err)
	}
	issue := func(actorID string, scopes []string) string {
		t.Helper()
		ws := wsID
		cred, err := svc.IssueCredential(t.Context(), auth.CreateCredentialInput{
			ActorID: actorID, Kind: "agent", Scopes: scopes, Workspace: &ws, CreatedBy: human.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return cred.Secret
	}
	fullRW := []string{auth.ScopeDocumentRead, auth.ScopeDocumentWrite, auth.ScopeMemoryRead, auth.ScopeMemoryWrite}
	return &docFixture{
		ts: ts, db: db, base: ts.URL + "/api/v1", wsID: wsID,
		fullA:    issue("agt_doc_a", fullRW),
		fullB:    issue("agt_doc_b", fullRW),
		readOnly: issue("agt_doc_ro", []string{auth.ScopeDocumentRead, auth.ScopeMemoryRead}),
		memOnly:  issue("agt_doc_mem", []string{auth.ScopeMemoryRead, auth.ScopeMemoryWrite}),
	}
}

// docDo 发请求并解析 JSON body（204 / 空 body 时 out 为 nil）。
func docDo(t *testing.T, method, url, token string, body any, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if strings.TrimSpace(resp.Header.Get("Content-Type")) != "" {
		_ = json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp.StatusCode, out, resp.Header
}

func testHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func docURL(f *docFixture, path string) string {
	return f.base + "/workspaces/" + f.wsID + "/documents/" + path
}

// push 发一次 PUT；返回 (状态, body, header)。
func push(t *testing.T, f *docFixture, token, path string, baseRevision int64, baseHash, content string, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	return docDo(t, http.MethodPut, docURL(f, path), token, map[string]any{
		"base_revision": baseRevision, "base_hash": baseHash, "content": content,
	}, headers)
}

func pushOK(t *testing.T, f *docFixture, token, path, content string) map[string]any {
	t.Helper()
	code, body, _ := push(t, f, token, path, 0, testHash(""), content, nil)
	if code != http.StatusOK {
		t.Fatalf("push %s: want 200, got %d %v", path, code, body)
	}
	return body
}

// errField 返回 error.details 的字段值（辅助断言）。
func errDetails(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	d, _ := e["details"].(map[string]any)
	return d
}

func errCode(t *testing.T, body map[string]any) string {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	c, _ := e["code"].(string)
	return c
}

func rev(t *testing.T, body map[string]any) int64 {
	t.Helper()
	v, ok := body["revision"].(float64)
	if !ok {
		t.Fatalf("missing revision: %v", body)
	}
	return int64(v)
}

func countAudit(t *testing.T, f *docFixture, action string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.AuditEntry{}).Where("action = ?", action).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func countOutbox(t *testing.T, f *docFixture, typ string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&model.OutboxEvent{}).Where("type = ?", typ).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- capabilities（S2）----

func TestCapabilitiesExposeDocumentSync(t *testing.T) {
	f := newDocFixture(t)
	code, caps, _ := docDo(t, http.MethodGet, f.base+"/meta/capabilities", "", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("capabilities: %d", code)
	}
	features := map[string]bool{}
	for _, v := range caps["features"].([]any) {
		features[v.(string)] = true
	}
	if !features["task_lease"] || !features["document_sync"] {
		t.Fatalf("features = %v, want task_lease + document_sync", caps["features"])
	}
}

// ---- §19-1 local-only：创建路径 + 三件套 ----

func TestDocumentLocalOnlyCreate(t *testing.T) {
	f := newDocFixture(t)
	body := pushOK(t, f, f.fullA, "docs/a.md", "v1 content")
	if rev(t, body) != 1 || body["deleted"] != false || body["content_hash"] != testHash("v1 content") {
		t.Fatalf("create response: %v", body)
	}

	// 三件套：领域行 + audit(document.push created=true) + outbox(document.updated)。
	var doc model.Document
	if err := f.db.Where("workspace_id = ? AND path = ?", f.wsID, "docs/a.md").First(&doc).Error; err != nil {
		t.Fatalf("document row: %v", err)
	}
	if doc.Revision != 1 || doc.DeletedAt != nil {
		t.Fatalf("row state: %+v", doc)
	}
	if n := countAudit(t, f, "document.push"); n != 1 {
		t.Fatalf("audit document.push rows = %d, want 1", n)
	}
	var entry model.AuditEntry
	if err := f.db.Where("action = ?", "document.push").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry.Details), `"created":true`) {
		t.Fatalf("audit details missing created=true: %s", entry.Details)
	}
	if n := countOutbox(t, f, "document.updated"); n != 1 {
		t.Fatalf("document.updated events = %d, want 1", n)
	}
	var evt model.OutboxEvent
	if err := f.db.Where("type = ?", "document.updated").First(&evt).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(evt.Payload), `"path":"docs/a.md"`) || !strings.Contains(string(evt.Payload), `"revision":1`) {
		t.Fatalf("event payload: %s", evt.Payload)
	}
}

// ---- §19-2 remote-only：get 404 ----

func TestDocumentRemoteOnlyGet404(t *testing.T) {
	f := newDocFixture(t)
	code, body, _ := docDo(t, http.MethodGet, docURL(f, "docs/missing.md"), f.fullA, nil, nil)
	if code != http.StatusNotFound || errCode(t, body) != "NOT_FOUND" {
		t.Fatalf("get missing: %d %v", code, body)
	}
}

// ---- §19-3 dual：快进同步推 + 陈旧 base 落冲突（双改） ----

func TestDocumentDualPushFastForwardThenConflict(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/dual.md", "v1")

	// 端 B 基于最新 base 快进（dual 非重叠：顺序同步成功）。
	code, body, _ := push(t, f, f.fullB, "docs/dual.md", 1, testHash("v1"), "v2", nil)
	if code != http.StatusOK || rev(t, body) != 2 || body["content"] != "v2" {
		t.Fatalf("fast-forward: %d %v", code, body)
	}

	// 端 A 持陈旧 base（1）再推（dual 重叠）→ 409 + 冲突工件。
	code, body, _ = push(t, f, f.fullA, "docs/dual.md", 1, testHash("v1"), "v1-merged-by-A", nil)
	if code != http.StatusConflict || errCode(t, body) != "DOCUMENT_CONFLICT" {
		t.Fatalf("stale push: %d %v", code, body)
	}
	details := errDetails(t, body)
	conflictID, _ := details["conflict_id"].(string)
	if conflictID == "" || details["current_revision"] != float64(2) || details["current_hash"] != testHash("v2") {
		t.Fatalf("conflict details: %v", details)
	}

	// 冲突列表（open）+ 详情（双方全文）。
	code, list, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts", f.fullA, nil, nil)
	if code != http.StatusOK || len(list["items"].([]any)) != 1 {
		t.Fatalf("conflicts list: %d %v", code, list)
	}
	item := list["items"].([]any)[0].(map[string]any)
	if item["id"] != conflictID || item["status"] != "open" || item["base_revision"] != float64(1) {
		t.Fatalf("conflict item: %v", item)
	}
	if item["ours_hash"] != testHash("v1-merged-by-A") || item["theirs_revision"] != float64(2) || item["theirs_hash"] != testHash("v2") {
		t.Fatalf("conflict hashes: %v", item)
	}
	code, detail, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts/"+conflictID, f.fullA, nil, nil)
	if code != http.StatusOK || detail["ours_content"] != "v1-merged-by-A" || detail["theirs_content"] != "v2" {
		t.Fatalf("conflict detail: %d %v", code, detail)
	}

	// document.conflict 事件 + audit（document.push details.conflict_id）。
	if n := countOutbox(t, f, "document.conflict"); n != 1 {
		t.Fatalf("document.conflict events = %d, want 1", n)
	}
	var entries []model.AuditEntry
	if err := f.db.Where("action = ?", "document.push").Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(string(e.Details), conflictID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no document.push audit row carries conflict_id %s (rows=%d)", conflictID, len(entries))
	}
}

// ---- §19-4/5 edit-vs-delete / delete-vs-edit ----

func TestDocumentDeleteVsEditConflict(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/de.md", "v1")

	// 端 B 删除（base 1）→ tombstone rev2 + 事件 data.deleted=true。
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/de.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullB)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	var evt model.OutboxEvent
	if err := f.db.Where("type = ?", "document.updated").Order("id DESC").First(&evt).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(evt.Payload), `"deleted":true`) {
		t.Fatalf("delete event payload: %s", evt.Payload)
	}

	// manifest 默认排除 tombstone；include_deleted=true 含之（deleted=true）。
	code, m, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest", f.fullA, nil, nil)
	if code != http.StatusOK || len(m["items"].([]any)) != 0 {
		t.Fatalf("manifest default should exclude tombstone: %v", m)
	}
	code, m, _ = docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest?include_deleted=true", f.fullA, nil, nil)
	items := m["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["deleted"] != true {
		t.Fatalf("manifest include_deleted: %v", m)
	}

	// get 仍返回 tombstone（deleted=true，content 照常）。
	code, body, _ := docDo(t, http.MethodGet, docURL(f, "docs/de.md"), f.fullA, nil, nil)
	if code != http.StatusOK || body["deleted"] != true || body["content"] != "v1" {
		t.Fatalf("get tombstone: %d %v", code, body)
	}

	// 端 A 持陈旧 base push（edit-vs-delete）→ 冲突，theirs 标记远端已删除。
	code, body, _ = push(t, f, f.fullA, "docs/de.md", 1, testHash("v1"), "v2-by-A", nil)
	if code != http.StatusConflict || errCode(t, body) != "DOCUMENT_CONFLICT" {
		t.Fatalf("edit-vs-delete: %d %v", code, body)
	}
	conflictID := errDetails(t, body)["conflict_id"].(string)
	code, detail, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts/"+conflictID, f.fullA, nil, nil)
	if code != http.StatusOK || detail["theirs_content"] != "v1" {
		t.Fatalf("conflict detail (edit-vs-delete): %d %v", code, detail)
	}
	var cfl model.DocumentConflict
	if err := f.db.Where("id = ?", conflictID).First(&cfl).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfl.TheirsJSON), `"deleted":true`) {
		t.Fatalf("theirs_json should mark remote deleted: %s", cfl.TheirsJSON)
	}
}

func TestDocumentEditVsDeleteConflict(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/ed.md", "v1")
	// 端 B 先编辑到 rev2。
	code, body, _ := push(t, f, f.fullB, "docs/ed.md", 1, testHash("v1"), "v2", nil)
	if code != http.StatusOK {
		t.Fatalf("edit to v2: %d %v", code, body)
	}
	// 端 A 持陈旧 base 删除（delete-vs-edit）→ 冲突，ours 是 delete 意图。
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/ed.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var conflictBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&conflictBody)
	if resp.StatusCode != http.StatusConflict || errCode(t, conflictBody) != "DOCUMENT_CONFLICT" {
		t.Fatalf("delete-vs-edit: %d %v", resp.StatusCode, conflictBody)
	}
	conflictID := errDetails(t, conflictBody)["conflict_id"].(string)

	code, detail, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts/"+conflictID, f.fullA, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, detail)
	}
	// delete 意图方无内容/无 hash：ours_content 空串、ours_hash 省略。
	if detail["ours_content"] != "" {
		t.Fatalf("ours_content = %v, want empty (delete intent)", detail["ours_content"])
	}
	if _, has := detail["ours_hash"]; has {
		t.Fatalf("ours_hash should be omitted for delete intent: %v", detail)
	}
	if detail["theirs_content"] != "v2" || detail["theirs_revision"] != float64(2) {
		t.Fatalf("theirs side: %v", detail)
	}
	var cfl model.DocumentConflict
	if err := f.db.Where("id = ?", conflictID).First(&cfl).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfl.OursJSON), `"delete":true`) {
		t.Fatalf("ours_json should carry delete intent: %s", cfl.OursJSON)
	}
	// 文档未被误删。
	code, body, _ = docDo(t, http.MethodGet, docURL(f, "docs/ed.md"), f.fullA, nil, nil)
	if code != http.StatusOK || body["deleted"] != false {
		t.Fatalf("document must survive a conflicted delete: %d %v", code, body)
	}
}

// ---- §19-6 重复删幂等 + 参数校验 ----

func TestDocumentDeleteIdempotentAndValidation(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/idem.md", "v1")

	delete := func(query string) int {
		req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/idem.md")+query, nil)
		req.Header.Set("Authorization", "Bearer "+f.fullA)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := delete("?base_revision=1"); code != http.StatusNoContent {
		t.Fatalf("first delete: %d", code)
	}
	// 已删且 base 匹配当前 revision（2）→ 幂等 204。
	if code := delete("?base_revision=2"); code != http.StatusNoContent {
		t.Fatalf("idempotent delete: %d", code)
	}
	var doc model.Document
	if err := f.db.Where("workspace_id = ? AND path = ?", f.wsID, "docs/idem.md").First(&doc).Error; err != nil {
		t.Fatal(err)
	}
	if doc.Revision != 2 || doc.DeletedAt == nil {
		t.Fatalf("revision/tombstone must not change on idempotent delete: %+v", doc)
	}
	// base 失配（陈旧）→ 冲突而非幂等。
	if code := delete("?base_revision=1"); code != http.StatusConflict {
		t.Fatalf("stale re-delete: %d", code)
	}
	// 参数缺失 / 非正整数 → 400。
	if code := delete(""); code != http.StatusBadRequest {
		t.Fatalf("missing base_revision: %d", code)
	}
	if code := delete("?base_revision=abc"); code != http.StatusBadRequest {
		t.Fatalf("non-integer base_revision: %d", code)
	}
	if code := delete("?base_revision=0"); code != http.StatusBadRequest {
		t.Fatalf("zero base_revision: %d", code)
	}
	// 删除不存在的文档 → 404。
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/never.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete missing: %d", resp.StatusCode)
	}
}

// ---- §19-7 tombstone 复活 ----

func TestDocumentTombstoneRevive(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/rev.md", "v1")
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/rev.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// base_revision=0 推 tombstone → 复活（deleted_at=NULL，revision 续增）。
	code, body, _ := push(t, f, f.fullA, "docs/rev.md", 0, testHash(""), "v2-revived", nil)
	if code != http.StatusOK || rev(t, body) != 3 || body["deleted"] != false || body["content"] != "v2-revived" {
		t.Fatalf("revive: %d %v", code, body)
	}
	code, m, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest", f.fullA, nil, nil)
	if code != http.StatusOK || len(m["items"].([]any)) != 1 || m["items"].([]any)[0].(map[string]any)["deleted"] != false {
		t.Fatalf("manifest after revive: %v", m)
	}
}

// ---- §19-8 hash 不符 → 400 ----

func TestDocumentHashMismatch400(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/hash.md", "v1")

	// revision 匹配但 base_hash 与该行当前 hash 不一致 → 400（本地状态错乱）。
	code, body, _ := push(t, f, f.fullA, "docs/hash.md", 1, testHash("different"), "v2", nil)
	if code != http.StatusBadRequest || errCode(t, body) != "VALIDATION_FAILED" {
		t.Fatalf("base_hash mismatch: %d %v", code, body)
	}
	if d := errDetails(t, body); d["field"] != "base_hash" || d["reason"] != "base_hash_mismatch" {
		t.Fatalf("base_hash mismatch details: %v", d)
	}

	// content_hash 与服务端重算值不符 → 400。
	code, body, _ = docDo(t, http.MethodPut, docURL(f, "docs/hash2.md"), f.fullA, map[string]any{
		"base_revision": 0, "base_hash": testHash(""), "content": "abc", "content_hash": testHash("xyz"),
	}, nil)
	if code != http.StatusBadRequest || errCode(t, body) != "VALIDATION_FAILED" {
		t.Fatalf("content_hash mismatch: %d %v", code, body)
	}
	if d := errDetails(t, body); d["field"] != "content_hash" || d["reason"] != "content_hash_mismatch" {
		t.Fatalf("content_hash mismatch details: %v", d)
	}
	// 非法 hash 格式 → 400。
	code, body, _ = docDo(t, http.MethodPut, docURL(f, "docs/hash3.md"), f.fullA, map[string]any{
		"base_revision": 0, "base_hash": "not-a-hash", "content": "abc",
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("malformed base_hash: %d %v", code, body)
	}
}

// ---- R1 大小写冲突 ----

func TestDocumentCaseCollision(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "Docs/a.md", "v1")

	code, body, _ := push(t, f, f.fullA, "docs/a.md", 0, testHash(""), "collide", nil)
	if code != http.StatusBadRequest || errCode(t, body) != "VALIDATION_FAILED" {
		t.Fatalf("case collision: %d %v", code, body)
	}
	d := errDetails(t, body)
	if d["field"] != "path" || d["reason"] != "path_case_collision" {
		t.Fatalf("case collision details: %v", d)
	}
	// 大小写不同的另一变体同样拒绝。
	if code, _, _ = push(t, f, f.fullA, "DOCS/A.MD", 0, testHash(""), "collide2", nil); code != http.StatusBadRequest {
		t.Fatalf("DOCS/A.MD should collide: %d", code)
	}
	// 精确匹配原路径不受影响（继续走正常分支而非 400 collision）。
	code, body, _ = push(t, f, f.fullA, "Docs/a.md", 1, testHash("v1"), "v2", nil)
	if code != http.StatusOK || rev(t, body) != 2 {
		t.Fatalf("exact re-push must not be treated as collision: %d %v", code, body)
	}
	// get/manifest 仍精确匹配：小写路径查不到（404）。
	code, _, _ = docDo(t, http.MethodGet, docURL(f, "docs/a.md"), f.fullA, nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("exact-match get of lowercased path: %d", code)
	}
}

// ---- 路径校验（§19 path traversal 的 HTTP 层） ----

func TestDocumentPathValidationOverHTTP(t *testing.T) {
	f := newDocFixture(t)
	// %2F 解码后成 "../evil" → traversal 400。
	code, body, _ := docDo(t, http.MethodPut, docURL(f, "..%2Fevil"), f.fullA, map[string]any{
		"base_revision": 0, "base_hash": testHash(""), "content": "x",
	}, nil)
	if code != http.StatusBadRequest || errDetails(t, body)["field"] != "path" {
		t.Fatalf("traversal: %d %v", code, body)
	}
	// 保留前缀 secrets/ → 400。
	code, body, _ = docDo(t, http.MethodPut, docURL(f, "secrets/key.txt"), f.fullA, map[string]any{
		"base_revision": 0, "base_hash": testHash(""), "content": "x",
	}, nil)
	if code != http.StatusBadRequest || errDetails(t, body)["reason"] != "reserved_prefix" {
		t.Fatalf("reserved prefix: %d %v", code, body)
	}
	// 前导 '/'（%2F 解码后绝对路径）→ 400。
	code, body, _ = docDo(t, http.MethodPut, docURL(f, "%2Fabs.md"), f.fullA, map[string]any{
		"base_revision": 0, "base_hash": testHash(""), "content": "x",
	}, nil)
	if code != http.StatusBadRequest || errDetails(t, body)["reason"] != "path_absolute" {
		t.Fatalf("absolute path: %d %v", code, body)
	}
}

// ---- scope 前缀两轴 ----

func TestDocumentScopeAxes(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "memory/project.md", "mem v1")
	pushOK(t, f, f.fullA, "notes/a.md", "doc v1")

	// 只读 credential（两轴 read）：两轴都能读，写全 403。
	code, body, _ := docDo(t, http.MethodGet, docURL(f, "memory/project.md"), f.readOnly, nil, nil)
	if code != http.StatusOK || body["content"] != "mem v1" {
		t.Fatalf("readOnly get memory: %d %v", code, body)
	}
	if code, _, _ = docDo(t, http.MethodGet, docURL(f, "notes/a.md"), f.readOnly, nil, nil); code != http.StatusOK {
		t.Fatalf("readOnly get notes: %d", code)
	}
	code, body, _ = push(t, f, f.readOnly, "notes/a.md", 1, testHash("doc v1"), "v2", nil)
	if code != http.StatusForbidden || errCode(t, body) != "INSUFFICIENT_SCOPE" {
		t.Fatalf("readOnly push notes: %d %v", code, body)
	}
	code, body, _ = push(t, f, f.readOnly, "memory/project.md", 1, testHash("mem v1"), "v2", nil)
	if code != http.StatusForbidden {
		t.Fatalf("readOnly push memory: %d %v", code, body)
	}

	// memory-only credential：memory/** 读写皆可，非 memory 路径读写全拒；
	// workspace 级清单（any-read 轴）可拉。
	code, body, _ = push(t, f, f.memOnly, "memory/new.md", 0, testHash(""), "m", nil)
	if code != http.StatusOK || rev(t, body) != 1 {
		t.Fatalf("memOnly push memory: %d %v", code, body)
	}
	code, body, _ = docDo(t, http.MethodGet, docURL(f, "notes/a.md"), f.memOnly, nil, nil)
	if code != http.StatusForbidden || errCode(t, body) != "INSUFFICIENT_SCOPE" {
		t.Fatalf("memOnly get notes: %d %v", code, body)
	}
	code, body, _ = push(t, f, f.memOnly, "notes/a.md", 1, testHash("doc v1"), "hack", nil)
	if code != http.StatusForbidden {
		t.Fatalf("memOnly push notes: %d %v", code, body)
	}
	code, body, _ = docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest", f.memOnly, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("memOnly manifest (any-read): %d %v", code, body)
	}
	// 非成员（绑定他处/无 credential）→ 404 不泄露存在性。
	req, _ := http.NewRequest(http.MethodGet, docURL(f, "notes/a.md"), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", resp.StatusCode)
	}
}

// ---- manifest 游标分页 + include_deleted ----

func TestManifestCursorPagination(t *testing.T) {
	f := newDocFixture(t)
	for _, p := range []string{"docs/a.md", "docs/b.md", "docs/c.md", "docs/d.md", "docs/e.md"} {
		pushOK(t, f, f.fullA, p, "x")
	}
	// 删一个：默认排除，include_deleted=true 含之。
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/b.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	fetch := func(query string) ([]string, string) {
		code, m, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest"+query, f.fullA, nil, nil)
		if code != http.StatusOK {
			t.Fatalf("manifest %q: %d", query, code)
		}
		items := m["items"].([]any)
		paths := make([]string, 0, len(items))
		for _, it := range items {
			paths = append(paths, it.(map[string]any)["path"].(string))
		}
		next := ""
		if v, ok := m["next_cursor"].(string); ok {
			next = v
		}
		return paths, next
	}

	paths, next := fetch("?limit=2")
	if len(paths) != 2 || paths[0] != "docs/a.md" || paths[1] != "docs/c.md" || next != "docs/c.md" {
		t.Fatalf("page1 = %v next=%q（tombstone b 应被排除）", paths, next)
	}
	paths, next = fetch("?limit=2&cursor=" + next)
	if len(paths) != 2 || paths[0] != "docs/d.md" || paths[1] != "docs/e.md" || next != "" {
		t.Fatalf("page2 = %v next=%q（末页 next 应为空）", paths, next)
	}
	// include_deleted=true：b 回到清单且 deleted=true。
	paths, _ = fetch("?include_deleted=true&limit=3")
	if len(paths) != 3 || paths[1] != "docs/b.md" {
		t.Fatalf("include_deleted page = %v", paths)
	}
	code, m, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/documents/manifest?include_deleted=true&cursor=docs/a.md&limit=1", f.fullA, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("include_deleted+cursor: %d", code)
	}
	item := m["items"].([]any)[0].(map[string]any)
	if item["path"] != "docs/b.md" || item["deleted"] != true || item["size"] != float64(1) {
		t.Fatalf("tombstone manifest item: %v", item)
	}
	// limit 越界回落上限、非法值回落缺省（不报错）。
	if paths, _ = fetch("?limit=99999"); len(paths) != 4 {
		t.Fatalf("over-max limit: %v", paths)
	}
	if paths, _ = fetch("?limit=abc"); len(paths) != 4 {
		t.Fatalf("invalid limit: %v", paths)
	}
}

// ---- 1MiB 内容上限 ----

func TestDocumentContentSizeLimit(t *testing.T) {
	f := newDocFixture(t)
	exact := strings.Repeat("a", 1<<20)
	code, body, _ := push(t, f, f.fullA, "docs/big.md", 0, testHash(exact), exact, nil)
	if code != http.StatusOK {
		t.Fatalf("exactly 1MiB should pass: %d %v", code, body)
	}
	over := strings.Repeat("a", 1<<20+1)
	code, body, _ = push(t, f, f.fullA, "docs/big2.md", 0, testHash(over), over, nil)
	if code != http.StatusBadRequest || errDetails(t, body)["field"] != "content" {
		t.Fatalf("over 1MiB: %d %v", code, body)
	}
}

// ---- resolve 四分支 + 已解决 409 + 详情 404 ----

// seedConflict 推 v1 后用陈旧 base 再推产生一个冲突工件，返回 conflict_id。
func seedConflict(t *testing.T, f *docFixture, path, oursContent string) string {
	t.Helper()
	pushOK(t, f, f.fullA, path, "remote v1")
	code, body, _ := push(t, f, f.fullB, path, 0, testHash(""), oursContent, nil)
	if code != http.StatusConflict {
		t.Fatalf("seed conflict: %d %v", code, body)
	}
	return errDetails(t, body)["conflict_id"].(string)
}

func resolve(t *testing.T, f *docFixture, conflictID string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	code, body, _ := docDo(t, http.MethodPost, f.base+"/workspaces/"+f.wsID+"/conflicts/"+conflictID+"/resolve", f.fullA, payload, nil)
	return code, body
}

func TestConflictResolveOurs(t *testing.T) {
	f := newDocFixture(t)
	id := seedConflict(t, f, "docs/r-ours.md", "ours final")
	before := countOutbox(t, f, "document.updated")

	code, body := resolve(t, f, id, map[string]any{"resolution": "ours"})
	if code != http.StatusOK || body["content"] != "ours final" || rev(t, body) != 2 {
		t.Fatalf("resolve ours: %d %v", code, body)
	}
	if countOutbox(t, f, "document.updated") != before+1 {
		t.Fatal("resolve ours must emit document.updated")
	}
	if n := countAudit(t, f, "document.resolve"); n != 1 {
		t.Fatalf("audit document.resolve = %d", n)
	}
	// 冲突关闭：列表 open 空、resolved 有；再 resolve → 409 already_resolved。
	code, list, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts", f.fullA, nil, nil)
	if code != http.StatusOK || len(list["items"].([]any)) != 0 {
		t.Fatalf("open list after resolve: %v", list)
	}
	code, list, _ = docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts?status=resolved", f.fullA, nil, nil)
	items := list["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["resolution"] != "ours" {
		t.Fatalf("resolved list: %v", list)
	}
	code, body = resolve(t, f, id, map[string]any{"resolution": "theirs"})
	if code != http.StatusConflict || errDetails(t, body)["reason"] != "already_resolved" {
		t.Fatalf("double resolve: %d %v", code, body)
	}
}

func TestConflictResolveTheirs(t *testing.T) {
	f := newDocFixture(t)
	id := seedConflict(t, f, "docs/r-theirs.md", "ours losing edit")
	before := countOutbox(t, f, "document.updated")

	code, body := resolve(t, f, id, map[string]any{"resolution": "theirs"})
	// 返回当前 Document：内容仍是远端版本、revision 不 bump。
	if code != http.StatusOK || body["content"] != "remote v1" || rev(t, body) != 1 {
		t.Fatalf("resolve theirs: %d %v", code, body)
	}
	if countOutbox(t, f, "document.updated") != before {
		t.Fatal("resolve theirs must NOT emit document.updated")
	}
	var cfl model.DocumentConflict
	if err := f.db.Where("id = ?", id).First(&cfl).Error; err != nil {
		t.Fatal(err)
	}
	if cfl.Resolution == nil || *cfl.Resolution != "theirs" || cfl.ResolvedAt == nil {
		t.Fatalf("conflict row not closed: %+v", cfl)
	}
}

func TestConflictResolveMergedAndManual(t *testing.T) {
	f := newDocFixture(t)
	id := seedConflict(t, f, "docs/r-merged.md", "ours edit")

	// merged/manual 缺 content → 400。
	code, body := resolve(t, f, id, map[string]any{"resolution": "merged"})
	if code != http.StatusBadRequest || errDetails(t, body)["field"] != "content" {
		t.Fatalf("merged without content: %d %v", code, body)
	}
	code, body = resolve(t, f, id, map[string]any{"resolution": "merged", "content": "merged text"})
	if code != http.StatusOK || body["content"] != "merged text" || rev(t, body) != 2 {
		t.Fatalf("resolve merged: %d %v", code, body)
	}
	if body["content_hash"] != testHash("merged text") {
		t.Fatalf("merged hash: %v", body["content_hash"])
	}

	id2 := seedConflict(t, f, "docs/r-manual.md", "ours edit 2")
	code, body = resolve(t, f, id2, map[string]any{"resolution": "manual", "content": "manual text"})
	if code != http.StatusOK || body["content"] != "manual text" || rev(t, body) != 2 {
		t.Fatalf("resolve manual: %d %v", code, body)
	}
	// 非法 resolution → 400。
	id3 := seedConflict(t, f, "docs/r-bad.md", "ours edit 3")
	code, body = resolve(t, f, id3, map[string]any{"resolution": "bogus"})
	if code != http.StatusBadRequest {
		t.Fatalf("bad resolution: %d %v", code, body)
	}
}

// resolve(ours) 于 delete 意图冲突：以删除意图落地 tombstone。
func TestConflictResolveOursDeleteIntent(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/r-del.md", "v1")
	code, body, _ := push(t, f, f.fullB, "docs/r-del.md", 1, testHash("v1"), "v2", nil)
	if code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, body)
	}
	req, _ := http.NewRequest(http.MethodDelete, docURL(f, "docs/r-del.md")+"?base_revision=1", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var conflictBody map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&conflictBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("seed delete conflict: %d %v", resp.StatusCode, conflictBody)
	}
	id := errDetails(t, conflictBody)["conflict_id"].(string)

	code, body = resolve(t, f, id, map[string]any{"resolution": "ours"})
	if code != http.StatusOK || body["deleted"] != true || rev(t, body) != 3 {
		t.Fatalf("resolve ours (delete intent): %d %v", code, body)
	}
	var evt model.OutboxEvent
	if err := f.db.Where("type = ?", "document.updated").Order("id DESC").First(&evt).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(evt.Payload), `"deleted":true`) {
		t.Fatalf("ours-delete resolve event: %s", evt.Payload)
	}
}

func TestConflictDetail404AndListValidation(t *testing.T) {
	f := newDocFixture(t)
	code, body, _ := docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts/cfl_missing", f.fullA, nil, nil)
	if code != http.StatusNotFound || errCode(t, body) != "NOT_FOUND" {
		t.Fatalf("conflict detail 404: %d %v", code, body)
	}
	code, body, _ = docDo(t, http.MethodGet, f.base+"/workspaces/"+f.wsID+"/conflicts?status=bogus", f.fullA, nil, nil)
	if code != http.StatusBadRequest || errDetails(t, body)["field"] != "status" {
		t.Fatalf("bad status filter: %d %v", code, body)
	}
}

// ---- push 幂等重放（Idempotency-Key）----

func TestPushIdempotentReplay(t *testing.T) {
	f := newDocFixture(t)
	headers := map[string]string{"Idempotency-Key": "doc-key-1"}
	code, body, h := push(t, f, f.fullA, "docs/idem-key.md", 0, testHash(""), "v1", headers)
	if code != http.StatusOK || rev(t, body) != 1 {
		t.Fatalf("first push: %d %v", code, body)
	}
	if h.Get("X-Astral-Idempotent-Replay") != "" {
		t.Fatal("first push must not be a replay")
	}

	// 同键重放：返回首次 2xx 响应（即使本次载荷不同也不会执行）。
	code, body, h = push(t, f, f.fullA, "docs/idem-key.md", 1, testHash("v1"), "v2-would-conflict", headers)
	if code != http.StatusOK || rev(t, body) != 1 || body["content"] != "v1" {
		t.Fatalf("replay: %d %v", code, body)
	}
	if h.Get("X-Astral-Idempotent-Replay") != "true" {
		t.Fatalf("replay header missing: %v", h)
	}
	// 落库状态未被第二次请求推进。
	var doc model.Document
	if err := f.db.Where("workspace_id = ? AND path = ?", f.wsID, "docs/idem-key.md").First(&doc).Error; err != nil {
		t.Fatal(err)
	}
	if doc.Revision != 1 || doc.Content != "v1" {
		t.Fatalf("state advanced by replayed push: %+v", doc)
	}
}
