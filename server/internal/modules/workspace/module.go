// Package workspace 模块（architecture §6.2、§11）。
package workspace

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	// 注意：路径一律扁平注册。chi 不允许多个模块重复 Route("/workspaces/{workspace_id}")
	// 子挂载（会 panic），共享前缀的端点由各模块直接写全路径。
	r.Post("/workspaces", m.create)
	r.Get("/workspaces", m.list)
	r.Get("/workspaces/{workspace_id}", m.get)
	r.Patch("/workspaces/{workspace_id}", m.update)
	r.Get("/workspaces/{workspace_id}/members", m.listMembers)
	r.Post("/workspaces/{workspace_id}/members", m.addMember)
	r.Patch("/workspaces/{workspace_id}/members/{actor_id}", m.updateMember)
	r.Delete("/workspaces/{workspace_id}/members/{actor_id}", m.removeMember)
	// agent identity/credential 管理挂在 workspace 下（architecture §9）
	r.Get("/workspaces/{workspace_id}/agents", m.listAgents)
	r.Post("/workspaces/{workspace_id}/agents", m.createAgent)
	r.Post("/agents/{agent_id}/credentials", m.createCredential)
	r.Delete("/agents/{agent_id}/credentials/{credential_id}", m.revokeCredential)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 创建 workspace。
	//  - name/slug 唯一约束冲突 → WORKSPACE_ALREADY_BOUND?（命名见 openapi：用 VALIDATION_FAILED + details）
	//  - 支持 Idempotency-Key；创建者自动成为 owner 成员；写 audit + outbox。
	//  - name/slug 精确解析供 astral init 使用（?name=<exact-or-slug>，见 list）。
	httpx.NotImplemented(w, r, "workspace.create", "phase-2", "docs/architecture.md §11")
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 列出当前 actor 可见的 workspace；
	//  支持 ?name=<exact-or-slug> 精确解析（astral init 依赖，必须区分 404 与无权限）。
	httpx.NotImplemented(w, r, "workspace.list", "phase-2", "docs/architecture.md §11")
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 单个 workspace 详情；init 最终绑定校验用它（astral-cli §9.3 最后一步）。
	httpx.NotImplemented(w, r, "workspace.get", "phase-2", "docs/architecture.md §11")
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): PATCH name/slug（workspace_id 不变，改名不破坏 CLI 绑定）。
	httpx.NotImplemented(w, r, "workspace.update", "phase-2", "docs/architecture.md §11")
}

func (m *Module) listMembers(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 成员列表（actor + role）。
	httpx.NotImplemented(w, r, "workspace.members.list", "phase-2", "docs/protocol.md §9")
}

func (m *Module) addMember(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 添加成员（workspace:manage_members scope）。
	//  membership.promote_owner 属高风险动作，走 approval 状态机（architecture §22）。
	httpx.NotImplemented(w, r, "workspace.members.add", "phase-2", "docs/protocol.md §9")
}

func (m *Module) updateMember(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 调整成员 role；owner 转让走 approval。
	httpx.NotImplemented(w, r, "workspace.members.update", "phase-2", "docs/protocol.md §9")
}

func (m *Module) removeMember(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 移除成员；同步撤销该成员在此 workspace 的活跃 lease/presence。
	httpx.NotImplemented(w, r, "workspace.members.remove", "phase-2", "docs/protocol.md §9")
}

func (m *Module) listAgents(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): workspace 内 agent/service actor 列表（agent:manage scope）。
	httpx.NotImplemented(w, r, "agents.list", "phase-2", "docs/architecture.md §9")
}

func (m *Module) createAgent(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 创建 agent actor。credential 创建分两步走（先建身份再签发），
	// 也可在此复合签发——实现前在 openapi 定稿一种，避免 CLI 假设错流程。
	httpx.NotImplemented(w, r, "agents.create", "phase-2", "docs/architecture.md §9")
}

func (m *Module) createCredential(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 签发 credential。明文 secret 只在本次响应出现一次；
	//  返回 credential_id + secret + scopes + expires_at；写 audit + outbox
	//  (security.credential.created)。credential.create_privileged / extend_long_ttl
	//  走 approval（architecture §22）。
	httpx.NotImplemented(w, r, "agents.credentials.create", "phase-2", "docs/architecture.md §9")
}

func (m *Module) revokeCredential(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-2): 撤销 credential：置 revoked_at + audit + outbox
	//  (security.credential.revoked) + 主动断开该 credential 的 SSE 连接。
	httpx.NotImplemented(w, r, "agents.credentials.revoke", "phase-2", "docs/architecture.md §9")
}
