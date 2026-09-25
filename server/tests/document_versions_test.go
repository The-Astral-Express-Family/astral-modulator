// document_versions_test.go 是历史版本链（00017）的 app 级 HTTP 集成测试：
// 四种取代原因（push 快进 / delete 进 tombstone / revive 复活 / resolve 裁决）
// 各自归档、读端点列表/详情/分页/校验、scope 两轴、以及「取旧版内容 +
// pinned base push」的恢复往返。
package tests

import (
	"net/http"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// versionsURL 拼版本列表端点；path 可为空（用于缺参用例）。
func versionsURL(f *docFixture, path string) string {
	u := f.base + "/workspaces/" + f.wsID + "/document-versions"
	if path != "" {
		u += "?path=" + path
	}
	return u
}

// versionItems 取 items 数组；失败即 fatal。
func versionItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("response missing items: %v", body)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		if m, ok := it.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

// ---- 四分支归档：push 快进 ×2 → delete → revive ----

func TestDocumentVersionsArchiveAcrossBranches(t *testing.T) {
	f := newDocFixture(t)

	// 创建 rev1（内容 A）：无取代，不归档。
	pushOK(t, f, f.fullA, "docs/hist.md", "A")
	// 快进 rev2（B）：归档 rev1(A, push)。
	if code, body, _ := push(t, f, f.fullA, "docs/hist.md", 1, testHash("A"), "B", nil); code != http.StatusOK || rev(t, body) != 2 {
		t.Fatalf("ff push to rev2: %d %v", code, body)
	}
	// 快进 rev3（C）：归档 rev2(B, push)。
	if code, body, _ := push(t, f, f.fullA, "docs/hist.md", 2, testHash("B"), "C", nil); code != http.StatusOK || rev(t, body) != 3 {
		t.Fatalf("ff push to rev3: %d %v", code, body)
	}
	// 删除 rev4：归档 rev3(C, delete, deleted=false 快照是删前的活行)。
	if code, _, _ := docDo(t, http.MethodDelete, docURL(f, "docs/hist.md")+"?base_revision=3", f.fullA, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	// 复活 rev5（D）：归档 rev4(C, revive, deleted=true 快照是 tombstone 行)。
	pushOK(t, f, f.fullA, "docs/hist.md", "D")

	code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/hist.md"), f.fullA, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("list versions: %d %v", code, body)
	}
	items := versionItems(t, body)
	if len(items) != 4 {
		t.Fatalf("versions = %d, want 4: %v", len(items), body)
	}
	// revision 降序 + 四字段逐项断言。
	want := []struct {
		rev     int64
		kind    string
		deleted bool
		hash    string
	}{
		{4, "revive", true, testHash("C")},  // 复活前身的 tombstone 行仍持 C
		{3, "delete", false, testHash("C")}, // 被删除取代的活行
		{2, "push", false, testHash("B")},
		{1, "push", false, testHash("A")},
	}
	for i, w := range want {
		it := items[i]
		if it["revision"].(float64) != float64(w.rev) || it["kind"] != w.kind ||
			it["deleted"] != w.deleted || it["content_hash"] != w.hash {
			t.Fatalf("item[%d] = %v, want rev=%d kind=%s deleted=%v hash=%s", i, it, w.rev, w.kind, w.deleted, w.hash)
		}
		if it["actor_id"] != "agt_doc_a" || it["path"] != "docs/hist.md" {
			t.Fatalf("item[%d] actor/path: %v", i, it)
		}
	}
	// 当前版本（rev5）不在历史里——历史只含被取代版本。
	for _, it := range items {
		if it["revision"].(float64) == 5 {
			t.Fatalf("current revision leaked into history: %v", it)
		}
	}

	// 详情：rev2 内容完整取回（{revision} 路径参数 + path query）。
	code, body, _ = docDo(t, http.MethodGet,
		f.base+"/workspaces/"+f.wsID+"/document-versions/2?path=docs/hist.md", f.fullA, nil, nil)
	if code != http.StatusOK || body["content"] != "B" || body["size"].(float64) != 1 || body["kind"] != "push" {
		t.Fatalf("get version 2: %d %v", code, body)
	}

	// 库层四件套抽查：归档行数与 kind 分布。
	var n int64
	if err := f.db.Model(&model.DocumentVersion{}).Count(&n).Error; err != nil || n != 4 {
		t.Fatalf("document_versions rows = %d err=%v, want 4", n, err)
	}
}

// ---- 分页 / 校验 / 404 ----

func TestDocumentVersionsPaginationAndValidation(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/pg.md", "v1")
	push(t, f, f.fullA, "docs/pg.md", 1, testHash("v1"), "v2", nil)
	push(t, f, f.fullA, "docs/pg.md", 2, testHash("v2"), "v3", nil)
	push(t, f, f.fullA, "docs/pg.md", 3, testHash("v3"), "v4", nil)

	// limit=2 → rev3, rev2；next_cursor = 2。
	code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/pg.md")+"&limit=2", f.fullA, nil, nil)
	items := versionItems(t, body)
	if code != http.StatusOK || len(items) != 2 || items[0]["revision"].(float64) != 3 || body["next_cursor"] != "2" {
		t.Fatalf("page1: %d %v", code, body)
	}
	// cursor=2 → 剩 rev1，next 为空（JSON null）。
	code, body, _ = docDo(t, http.MethodGet, versionsURL(f, "docs/pg.md")+"&limit=2&cursor=2", f.fullA, nil, nil)
	items = versionItems(t, body)
	if code != http.StatusOK || len(items) != 1 || items[0]["revision"].(float64) != 1 || body["next_cursor"] != nil {
		t.Fatalf("page2: %d %v", code, body)
	}

	// 文档不存在 → 404（与 get 同语义）。
	if code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/missing.md"), f.fullA, nil, nil); code != http.StatusNotFound || errCode(t, body) != "NOT_FOUND" {
		t.Fatalf("missing doc: %d %v", code, body)
	}
	// 当前版本（rev4）不在历史 → 404；rev3 已被取代，可取。
	if code, _, _ := docDo(t, http.MethodGet,
		f.base+"/workspaces/"+f.wsID+"/document-versions/4?path=docs/pg.md", f.fullA, nil, nil); code != http.StatusNotFound {
		t.Fatalf("current rev as version: %d", code)
	}
	if code, _, _ := docDo(t, http.MethodGet,
		f.base+"/workspaces/"+f.wsID+"/document-versions/3?path=docs/pg.md", f.fullA, nil, nil); code != http.StatusOK {
		t.Fatalf("superseded rev as version: %d", code)
	}
	// 缺 path / 非法 cursor / 非法 revision → 400。
	if code, body, _ := docDo(t, http.MethodGet, versionsURL(f, ""), f.fullA, nil, nil); code != http.StatusBadRequest || errCode(t, body) != "VALIDATION_FAILED" {
		t.Fatalf("missing path: %d %v", code, body)
	}
	if code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/pg.md")+"&cursor=abc", f.fullA, nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad cursor: %d %v", code, body)
	}
	if code, _, _ := docDo(t, http.MethodGet,
		f.base+"/workspaces/"+f.wsID+"/document-versions/0?path=docs/pg.md", f.fullA, nil, nil); code != http.StatusBadRequest {
		t.Fatalf("revision 0: %d", code)
	}
}

// ---- scope 两轴：document 轴读写分离 + memory 前缀轴 ----

func TestDocumentVersionsScopeAxes(t *testing.T) {
	f := newDocFixture(t)
	pushOK(t, f, f.fullA, "docs/scoped.md", "v1")
	push(t, f, f.fullA, "docs/scoped.md", 1, testHash("v1"), "v2", nil)

	// readOnly（document:read）可读历史。
	if code, _, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/scoped.md"), f.readOnly, nil, nil); code != http.StatusOK {
		t.Fatalf("readOnly list: %d", code)
	}
	// memOnly（仅 memory 轴）读普通文档历史 → 403。
	if code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "docs/scoped.md"), f.memOnly, nil, nil); code != http.StatusForbidden || errCode(t, body) != "INSUFFICIENT_SCOPE" {
		t.Fatalf("memOnly on doc path: %d %v", code, body)
	}
	// memory/ 前缀：memOnly 自己写、自己读历史。
	pushOK(t, f, f.memOnly, "memory/notes.md", "m1")
	push(t, f, f.memOnly, "memory/notes.md", 1, testHash("m1"), "m2", nil)
	if code, body, _ := docDo(t, http.MethodGet, versionsURL(f, "memory/notes.md"), f.memOnly, nil, nil); code != http.StatusOK || len(versionItems(t, body)) != 1 {
		t.Fatalf("memOnly memory history: %d %v", code, body)
	}
}

// ---- resolve 分支归档 + 恢复往返 ----

func TestConflictResolveArchivesVersionAndRestoreRoundtrip(t *testing.T) {
	f := newDocFixture(t)
	// seedConflict 落 v1 后以 base=0 盲推产生冲突（不快进，无 push 归档）。
	id := seedConflict(t, f, "docs/conf.md", "ours losing edit")

	// merged 裁决 → rev2，归档 rev1(remote v1, resolve)。
	code, body := resolve(t, f, id, map[string]any{"resolution": "merged", "content": "merged"})
	if code != http.StatusOK || rev(t, body) != 2 {
		t.Fatalf("resolve merged: %d %v", code, body)
	}
	code, body, _ = docDo(t, http.MethodGet, versionsURL(f, "docs/conf.md"), f.fullA, nil, nil)
	items := versionItems(t, body)
	if len(items) != 1 || items[0]["kind"] != "resolve" || items[0]["content_hash"] != testHash("remote v1") {
		t.Fatalf("post-resolve versions: %v", items)
	}

	// 恢复往返：取 rev1 全文（被裁决覆盖的远端内容），以 pinned base 推回。
	code, body, _ = docDo(t, http.MethodGet,
		f.base+"/workspaces/"+f.wsID+"/document-versions/1?path=docs/conf.md", f.fullA, nil, nil)
	if code != http.StatusOK || body["content"] != "remote v1" {
		t.Fatalf("get superseded version: %d %v", code, body)
	}
	code, body, _ = push(t, f, f.fullA, "docs/conf.md", 2, testHash("merged"), "remote v1", nil)
	if code != http.StatusOK || rev(t, body) != 3 {
		t.Fatalf("restore push: %d %v", code, body)
	}
	// 恢复本身也归档了被取代的 rev2(merged, push)。
	code, body, _ = docDo(t, http.MethodGet, versionsURL(f, "docs/conf.md"), f.fullA, nil, nil)
	items = versionItems(t, body)
	if len(items) != 2 || items[0]["kind"] != "push" || items[0]["content_hash"] != testHash("merged") {
		t.Fatalf("post-restore versions: %v", items)
	}
}
