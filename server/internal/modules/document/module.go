// Package document 模块：受管 Markdown 同步（architecture §16、sync-semantics.md）。
package document

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/documents/manifest", m.manifest)
	// {path:.*} 必须严格 URL 编码；服务端 canonicalize（'/'分隔），
	// 拒绝 '..'、绝对路径、symlink escape、secrets 默认路径（architecture §16）。
	// memory/ 前缀走 memory:read/write scope（M1 裁决，见 TODO.md 决策速查）。
	// manifest 是静态段，chi 优先于 {path:.*} 通配。
	r.Get("/workspaces/{workspace_id}/documents/{path:.*}", m.get)
	r.Put("/workspaces/{workspace_id}/documents/{path:.*}", m.push)
	r.Delete("/workspaces/{workspace_id}/documents/{path:.*}", m.delete)
	r.Get("/workspaces/{workspace_id}/conflicts", m.listConflicts)
	r.Get("/workspaces/{workspace_id}/conflicts/{conflict_id}", m.getConflict)
	r.Post("/workspaces/{workspace_id}/conflicts/{conflict_id}/resolve", m.resolveConflict)
}

func (m *Module) manifest(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 全量 manifest（path/revision/content_hash/size/updated_at）。
	//  content_hash 统一 'sha256:<hex>' 格式——CLI 端本地 hash 算法必须与此一致，
	//  这是双端对接最易出错处，联调时优先做契约测试。
	httpx.NotImplemented(w, r, "document.manifest", "phase-5", "docs/protocol.md §1；api/openapi.yaml documents")
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 取单文档（含 revision/hash/deleted）；路径非法 → VALIDATION_FAILED。
	httpx.NotImplemented(w, r, "document.get", "phase-5", "docs/protocol.md §1；api/openapi.yaml documents")
}

func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 版本化删除（tombstone，round 37 契约）：?base_revision=N
	//  匹配当前版本 → deleted_at 落地（revision+1，document.updated data.deleted=true）；
	//  远端已变更 → 409 DOCUMENT_CONFLICT 并落 conflict artifact（delete-vs-edit）；
	//  已是 tombstone 且 base 匹配 → 幂等 204。禁止无版本盲删。
	httpx.NotImplemented(w, r, "document.delete", "phase-5", "docs/sync-semantics.md §14；api/openapi.yaml deleteDocument")
}

func (m *Module) push(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 三方同步 push（round 37 裁决：MVP 不做服务端自动合并）：
	//  - 校验 base_revision/base_hash；与当前版本一致 → 写入 revision+1；
	//  - 双改（base 不匹配）→ 一律落 document_conflicts 并返回 409 DOCUMENT_CONFLICT
	//    （禁止 last-write-wins 静默覆盖；diff3 自动合并不进 MVP，选型项关闭，
	//    客户端可在本地合并后经 resolve(resolution=merged) 提交最终内容）；
	//  - 支持 Idempotency-Key；写 outbox(document.updated / document.conflict)。
	httpx.NotImplemented(w, r, "document.push", "phase-5", "docs/architecture.md §16；docs/sync-semantics.md §9/§13")
}

func (m *Module) listConflicts(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 冲突列表（status=open|resolved|all，created_at 降序游标分页）。
	httpx.NotImplemented(w, r, "document.conflicts.list", "phase-5", "docs/protocol.md §1；api/openapi.yaml documents")
}

func (m *Module) getConflict(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 冲突详情（DocumentConflictDetail：列表形状 + ours/theirs 全文），
	//  解决视图的对比数据源。
	httpx.NotImplemented(w, r, "document.conflict.get", "phase-5", "api/openapi.yaml getConflict")
}

func (m *Module) resolveConflict(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 解决冲突：resolution=ours|theirs|merged|manual，产生新 revision。
	httpx.NotImplemented(w, r, "document.conflicts.resolve", "phase-5", "docs/protocol.md §1；api/openapi.yaml documents")
}
