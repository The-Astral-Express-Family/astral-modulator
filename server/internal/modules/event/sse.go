package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// SSEHandler 提供工作区事件流端点（docs/protocol.md §5）：
//
//	GET /api/v1/workspaces/{workspace_id}/events
//
// 语义：先按 Last-Event-ID 从 outbox 补发（保留窗口见 RetentionWindow），
// 再接入 dispatcher 实时流；游标超窗下发 snapshot.required 后断流。
// Last-Event-ID 来源：请求头（CLI）或 last_event_id query 参数（浏览器
// EventSource 无法携带自定义头）。
type SSEHandler struct {
	Hub *Hub
	// DB 为 nil 时（无数据库桩模式）不支持重放，仅实时流。
	DB *gorm.DB
	// Auth 用于订阅前的 workspace 级授权（非成员 404 / scope 不足 403）。
	// 装配层必须挂载；nil 时端点 fail closed（503）。
	Auth *auth.Service
}

// replayBatch 是单批补发行数；批间 flush，避免大窗口一次性占用内存。
const replayBatch = 500

func (h *SSEHandler) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/events", h.stream)
}

// writeEvent 输出一条 SSE 事件（id/event/data + 空行）。返回 false = 客户端断开。
func writeEvent(w http.ResponseWriter, flusher http.Flusher, env Envelope) bool {
	data, err := json.Marshal(env)
	if err != nil {
		return true // 序列化失败属于服务端 bug，丢事件优于断流
	}
	if _, werr := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", env.ID, env.Type, data); werr != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (h *SSEHandler) stream(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspace_id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, &httpx.APIError{
			Status:  http.StatusInternalServerError,
			Code:    httpx.CodeInternalError,
			Message: "streaming unsupported",
		})
		return
	}

	// 订阅前的 workspace 级授权（TODO.md §11 第 10 项）：非成员 404
	// （不泄露存在性）/ scope 不足 403，语义与其余 workspace 端点一致。
	// 撤销后断流由 Hub.DisconnectActor 覆盖（auth.OnRevoke 装配）。
	if h.Auth == nil {
		httpx.WriteError(w, r, httpx.Unavailable("event auth not wired"))
		return
	}
	if apiErr := auth.RequireWorkspace(r, h.Auth, workspaceID, auth.ScopeWorkspaceRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}

	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("last_event_id")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 1. 先订阅（补发期间 dispatcher 投递进入缓冲），补发结束后按游标去重。
	// 订阅携带 actor 身份：凭证/会话撤销时经 Hub.DisconnectActor 主动断流。
	var actorID string
	if p := auth.PrincipalFrom(r.Context()); p != nil {
		actorID = p.ActorID
	}
	events, unsub := h.Hub.Subscribe(Subscription{ActorID: actorID, Filter: WorkspaceFilter(workspaceID)})
	defer unsub()

	if lastEventID != "" && h.DB != nil {
		gap, checkErr := h.cursorHasGap(r, workspaceID, lastEventID)
		switch {
		case checkErr != nil:
			// 检查失败：降级为不补发（实时流继续），SSE 注释行说明。
			fmt.Fprintf(w, ": replay unavailable (%v)\n\n", checkErr)
			flusher.Flush()
		case gap:
			// 超窗：下发控制事件并断流，客户端重新拉快照。
			writeEvent(w, flusher, Envelope{
				ID:            "evt_snapshot_required",
				Type:          outbox.TypeSnapshotRequired,
				WorkspaceID:   workspaceID,
				OccurredAt:    time.Now().UTC().Format(time.RFC3339),
				SchemaVersion: 1,
				Data: map[string]any{
					"reason":          "cursor_expired",
					"retention_hours": int(RetentionWindow.Hours()),
				},
			})
			return
		default:
			// 重放推进去重前沿：replay 与缓冲中的实时投递有重叠区间，
			// 补发到哪一行，实时流就从哪一行之后放行（见下方循环过滤）。
			lastReplayed := h.replay(w, flusher, workspaceID, lastEventID)
			if lastReplayed != "" {
				lastEventID = lastReplayed
			}
		}
	}

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
			// 补发与缓冲重叠期去重：只放行晚于游标的事件。
			// 注：UUIDv7 同毫秒内非严格单调，极小概率跨毫秒事件被误去重；
			// MVP 接受（客户端幂等消费）。
			if lastEventID != "" && strings.Compare(env.ID, lastEventID) <= 0 {
				continue
			}
			if !writeEvent(w, flusher, env) {
				return
			}
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// cursorHasGap 判断游标是否落在保留窗口之外：
//   - 游标格式非法（非 evt_ 前缀）→ true，无法安全补发；
//   - 游标行仍存在（窗口内的已投递行保留 24h）→ false；
//   - 游标行不存在且早于窗口内最老一行 → true（cursor 与窗口之间有事件被清理）；
//   - 游标晚于全部保留行 → false（游标在前沿，直接续实时流）。
//
// 依赖 outbox.id 为时间可排序的 evt_<uuidv7>（字符串比较即时间比较）。
func (h *SSEHandler) cursorHasGap(r *http.Request, workspaceID, cursor string) (bool, error) {
	if !strings.HasPrefix(cursor, "evt_") {
		return true, nil
	}
	var count int64
	err := h.DB.WithContext(r.Context()).Model(&model.OutboxEvent{}).
		Where("id = ? AND workspace_id = ?", cursor, workspaceID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	var oldest model.OutboxEvent
	err = h.DB.WithContext(r.Context()).
		Where("workspace_id = ?", workspaceID).
		Order("id ASC").First(&oldest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil // 窗口内无行，没有可丢的区间
	}
	if err != nil {
		return false, err
	}
	return strings.Compare(cursor, oldest.ID) < 0, nil
}

// replay 按 id 升序批量补发 (cursor, +inf) 的已投递事件。
// 返回实际补发到的最后一行 id：查询出错或客户端断开时为止损边界，
// 调用方用它推进实时流的去重前沿，并输出注释行说明中断。
func (h *SSEHandler) replay(w http.ResponseWriter, flusher http.Flusher, workspaceID, cursor string) (last string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		var rows []model.OutboxEvent
		err := h.DB.WithContext(ctx).
			Where("id > ? AND workspace_id = ?", cursor, workspaceID).
			Order("id ASC").Limit(replayBatch).Find(&rows).Error
		if err != nil {
			fmt.Fprintf(w, ": replay interrupted (%v)\n\n", err)
			flusher.Flush()
			return last
		}
		for _, row := range rows {
			env, convErr := envelopeFromRow(row)
			if convErr != nil {
				continue
			}
			if !writeEvent(w, flusher, env) {
				return last // 客户端断开
			}
			cursor, last = row.ID, row.ID
		}
		if len(rows) < replayBatch {
			return last
		}
	}
}
