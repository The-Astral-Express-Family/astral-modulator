// Package message 模块：Actor 私信/广播/task thread（architecture §6.7）。
package message

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspace_id}/messages", m.send)
	r.Get("/workspaces/{workspace_id}/messages", m.list)
}

func (m *Module) send(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-4): 发送消息（target: actor|workspace|task）。
	//  - 支持 Idempotency-Key（网络重试不得双发）；
	//  - 写 outbox(message.created)；收件人实时提示走事件流。
	httpx.NotImplemented(w, r, "message.send", "phase-4", "docs/protocol.md §12")
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-4): 消息列表（按 target/thread 过滤 + cursor 分页）。
	//  私信可见性：只允许 sender 与目标 actor 读取。
	httpx.NotImplemented(w, r, "message.list", "phase-4", "docs/protocol.md §12")
}
