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
	// manifest 是静态段，chi 优先于 {path:.*} 通配。
	r.Get("/workspaces/{workspace_id}/documents/{path:.*}", m.get)
	r.Put("/workspaces/{workspace_id}/documents/{path:.*}", m.push)
	r.Get("/workspaces/{workspace_id}/conflicts", m.listConflicts)
	r.Post("/workspaces/{workspace_id}/conflicts/{conflict_id}/resolve", m.resolveConflict)
}

func (m *Module) manifest(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 全量 manifest（path/revision/content_hash/size/updated_at）。
	//  content_hash 统一 'sha256:<hex>' 格式——CLI 端本地 hash 算法必须与此一致，
	//  这是双端对接最易出错处，联调时优先做契约测试。
	httpx.NotImplemented(w, r, "document.manifest", "phase-5", "docs/protocol.md §13")
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 取单文档（含 revision/hash）；路径非法 → VALIDATION_FAILED。
	httpx.NotImplemented(w, r, "document.get", "phase-5", "docs/protocol.md §13")
}

func (m *Module) push(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 三方同步 push：
	//  - 校验 base_revision/base_hash；本地远端同源双改 → 可合并则合并，否则落
	//    document_conflicts 并返回 DOCUMENT_CONFLICT（禁止 last-write-wins 静默覆盖）；
	//  - 支持 Idempotency-Key；写 outbox(document.updated / document.conflict)；
	//  - TODO(phase-5): diff3 合并算法选型（sync-semantics.md）。
	httpx.NotImplemented(w, r, "document.push", "phase-5", "docs/architecture.md §16")
}

func (m *Module) listConflicts(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 未解决冲突列表（GUI 冲突视图数据源）。
	httpx.NotImplemented(w, r, "document.conflicts.list", "phase-5", "docs/protocol.md §13")
}

func (m *Module) resolveConflict(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-5): 解决冲突：resolution=ours|theirs|merged|manual，产生新 revision。
	httpx.NotImplemented(w, r, "document.conflicts.resolve", "phase-5", "docs/protocol.md §13")
}
