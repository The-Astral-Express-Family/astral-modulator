// Package presence 模块：短生命周期展示状态（architecture §6.8）。
// Presence 不是所有权；只有 task lease 是。
package presence

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Put("/workspaces/{workspace_id}/presence/me", m.heartbeat)
	r.Get("/workspaces/{workspace_id}/presence", m.list)
}

func (m *Module) heartbeat(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-4): PUT presence/me。
	//  - ttl_seconds 服务端钳制（如 30~300）；expires_at < now() 视为 offline（不落库状态）；
	//  - 声称 working 不代表持有 lease —— 不校验也不隐含任务所有权；
	//  - 写 outbox(actor.presence.changed)。
	httpx.NotImplemented(w, r, "presence.heartbeat", "phase-4", "docs/protocol.md §11")
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-4): workspace 在线状态总览（GUI 首页数据源）。
	httpx.NotImplemented(w, r, "presence.list", "phase-4", "docs/protocol.md §11")
}
