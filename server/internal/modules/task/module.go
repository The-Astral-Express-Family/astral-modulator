// Package task 模块：TODO 树、claim/lease、搜索（architecture §12/§13/§17）。
package task

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspace_id}/tasks", m.create)
	r.Get("/workspaces/{workspace_id}/tasks", m.list)
	// search 是静态段，chi 优先匹配字面量，不会被其他动态段吞掉。
	r.Get("/workspaces/{workspace_id}/tasks/search", m.search)
	r.Get("/tasks/{task_id}", m.get)
	r.Patch("/tasks/{task_id}", m.update)
	r.Post("/tasks/{task_id}/claim", m.claim)
	r.Post("/tasks/{task_id}/lease/renew", m.renewLease)
	r.Delete("/tasks/{task_id}/lease", m.release)
	r.Get("/tasks/{task_id}/messages", m.taskMessages)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 创建 task。
	//  - parent 必须同 workspace（触发器兜底，应用层先校验并给友好错误）；
	//  - 支持 Idempotency-Key；revision=1；写 audit + outbox(task.created)。
	httpx.NotImplemented(w, r, "task.create", "phase-3", "docs/architecture.md §12")
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 任务列表（结构化过滤 + cursor 分页 envelope {items,next_cursor}）。
	httpx.NotImplemented(w, r, "task.list", "phase-3", "docs/protocol.md §5")
}

func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 搜索语义固定为（architecture §13）：
	//  权限过滤 -> 结构化过滤(parent/tag/status) -> regex(POSIX) -> fuzzy(pg_trgm 排序) -> 分页。
	//  安全硬要求：regex 输入长度上限、statement_timeout，防 ReDoS 拖垮库。
	//  CLI 参数：--regex 与 --fuzzy 可同时传；只传 regex 不做模糊排序。
	httpx.NotImplemented(w, r, "task.search", "phase-3", "docs/architecture.md §13")
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 单任务详情（含祖先路径 repo/docs/title 的动态计算，不做存储）。
	httpx.NotImplemented(w, r, "task.get", "phase-3", "docs/architecture.md §12")
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): PATCH 部分字段。
	//  - 必须带 expected_revision，不匹配 → 409 REVISION_CONFLICT + 当前 revision；
	//  - 改 parent 需做循环检测（recursive CTE）；
	//  - 删除语义另行设计（暂不提供 DELETE —— 删除父任务默认不级联，先拒绝）。
	httpx.NotImplemented(w, r, "task.update", "phase-3", "docs/architecture.md §12")
}

func (m *Module) claim(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 原子 claim —— 单条条件 UPDATE：
	//    UPDATE task_leases ... WHERE task_id=$1 AND (无租约 OR expires_at<now())
	//  并在同一事务校验 revision（expected_revision）、写 audit + outbox(task.claimed)。
	//  并发竞争只有一个成功 → 409 TASK_ALREADY_CLAIMED（含 holder 信息）。
	//  这是 Phase 1 spike 的验收核心（roadmap exit criteria）。
	httpx.NotImplemented(w, r, "task.claim", "phase-3", "docs/architecture.md §17")
}

func (m *Module) renewLease(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 续租：仅 holder 可续；过期后续租 → TASK_LEASE_EXPIRED，需重新 claim。
	httpx.NotImplemented(w, r, "task.lease.renew", "phase-3", "docs/architecture.md §17")
}

func (m *Module) release(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 释放：holder 或 task:override（human force release 走 approval 候选）。
	httpx.NotImplemented(w, r, "task.lease.release", "phase-3", "docs/architecture.md §17")
}

func (m *Module) taskMessages(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-4): task thread 消息列表（委托 message 模块）。
	httpx.NotImplemented(w, r, "task.messages.list", "phase-4", "docs/protocol.md §12")
}
