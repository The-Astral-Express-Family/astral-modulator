package event

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// SSEHandler 提供工作区事件流端点（docs/protocol.md §5）：
//
//	GET /api/v1/workspaces/{workspace_id}/events
//
// 本文件是真实实现（含 keepalive），CLI/GUI 从第一天起就能对它开发；
// 缺的是 outbox 重放（见 hub.go 的 TODO(phase-4)）与鉴权接入。
type SSEHandler struct {
	Hub *Hub
}

func (h *SSEHandler) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/events", h.stream)
}

func (h *SSEHandler) stream(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspace_id")
	// TODO(phase-1): 鉴权（event 订阅按 workspace:read scope 校验）+ actor 对该
	// workspace 的可见性过滤；revoke 后主动断流（security.md）。

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, &httpx.APIError{
			Status:  http.StatusInternalServerError,
			Code:    httpx.CodeInternalError,
			Message: "streaming unsupported",
		})
		return
	}

	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		// TODO(phase-4): outbox 重放窗口内补发 lastEventID 之后的事件；
		// 超出保留窗口时下发 snapshot.required 事件并关闭流。
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Astral-Request-Id / Protocol-Version 已由中间件统一写入。

	events, unsub := h.Hub.Subscribe(WorkspaceFilter(workspaceID))
	defer unsub()

	keepalive := time.NewTicker(KeepaliveInterval)
	defer keepalive.Stop()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case env, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(env)
			if err != nil {
				continue // envelope 序列化失败属于服务端 bug，丢事件优于断流
			}
			fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", env.ID, env.Type, data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
