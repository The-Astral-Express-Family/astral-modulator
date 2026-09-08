// Package message 模块：Actor 私信 / workspace 广播 / task thread（architecture §6.7）。
package message

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
)

type Module struct {
	DB   *gorm.DB
	Auth *auth.Service
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspace_id}/messages", m.send)
	r.Get("/workspaces/{workspace_id}/messages", m.list)
}

type messageDTO struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	ThreadID    *string         `json:"thread_id"`
	SenderID    string          `json:"sender_id"`
	TargetType  string          `json:"target_type"`
	TargetID    string          `json:"target_id"`
	Body        string          `json:"body"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

func toMessageDTO(row model.Message) messageDTO {
	return messageDTO{
		ID: row.ID, WorkspaceID: row.WorkspaceID, ThreadID: row.ThreadID,
		SenderID: row.SenderID, TargetType: row.TargetType, TargetID: row.TargetID,
		Body: row.Body, Metadata: json.RawMessage(row.Metadata),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// requireWorkspace：workspace 级端点的授权前置（非成员 404 / scope 不足 403）。
func (m *Module) requireWorkspace(r *http.Request, wsID string, need string) *httpx.APIError {
	p := auth.PrincipalFrom(r.Context())
	_, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, wsID, need)
	return apiErr
}

func (m *Module) send(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeMessageSend); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		Target struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"target"`
		ThreadID *string        `json:"thread_id"`
		Body     string         `json:"body"`
		Metadata map[string]any `json:"metadata"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || len(in.Body) > 20000 {
		httpx.WriteError(w, r, httpx.Invalid("body required (1-20000 chars)"))
		return
	}
	if in.Target.ID == "" {
		// 缺省 workspace 广播。
		in.Target.Type = "workspace"
		in.Target.ID = wsID
	}
	switch in.Target.Type {
	case "actor":
		// 私信收件人必须在本 workspace 可达，否则收件人永远读不到这条消息：
		//   - workspace 成员（human）；或
		//   - agent/service：持有效 credential 且绑定本 workspace（未绑定 = 全局）。
		var actor model.Actor
		err := m.DB.WithContext(r.Context()).First(&actor, "id = ?", in.Target.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, r, httpx.NotFound("target actor not found"))
			return
		}
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		reachable, err := m.reachableInWorkspace(r.Context(), wsID, in.Target.ID)
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		if !reachable {
			httpx.WriteError(w, r, httpx.NotFound("target actor is not reachable in this workspace"))
			return
		}
	case "workspace":
		in.Target.ID = wsID
	case "task":
		var t model.Task
		if err := m.DB.WithContext(r.Context()).First(&t, "id = ?", in.Target.ID).Error; err != nil || t.WorkspaceID != wsID {
			httpx.WriteError(w, r, httpx.NotFound("target task not found in workspace"))
			return
		}
	default:
		httpx.WriteError(w, r, httpx.Invalid("target.type must be actor|workspace|task"))
		return
	}
	var thread *string
	if in.ThreadID != nil && *in.ThreadID != "" {
		var parent model.Message
		err := m.DB.WithContext(r.Context()).First(&parent, "id = ?", *in.ThreadID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, r, httpx.NotFound("thread_id not found"))
			return
		}
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		thread = in.ThreadID
	}
	meta, _ := json.Marshal(in.Metadata)

	row := model.Message{
		ID: ids.New(ids.Message), WorkspaceID: wsID, ThreadID: thread,
		TargetType: in.Target.Type, TargetID: in.Target.ID,
		SenderID: p.ActorID, Body: in.Body, Metadata: meta,
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeMessageCreated, wsID, p.ActorID, 0, map[string]any{
			"message_id": row.ID, "target_type": row.TargetType, "target_id": row.TargetID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, toMessageDTO(row))
}

// reachableInWorkspace 判断 actor 能否读到本 workspace 的私信：
// 成员关系（human 常规路径）或有效 credential 绑定（agent 路径；
// credential 未绑定 workspace 视为全局 agent，可达）。
func (m *Module) reachableInWorkspace(ctx context.Context, wsID, actorID string) (bool, error) {
	var member int64
	if err := m.DB.WithContext(ctx).Model(&model.WorkspaceMember{}).
		Where("workspace_id = ? AND actor_id = ?", wsID, actorID).Count(&member).Error; err != nil {
		return false, err
	}
	if member > 0 {
		return true, nil
	}
	var bound int64
	if err := m.DB.WithContext(ctx).Model(&model.Credential{}).
		Where("actor_id = ? AND revoked_at IS NULL AND (workspace_id IS NULL OR workspace_id = ?)", actorID, wsID).
		Count(&bound).Error; err != nil {
		return false, err
	}
	return bound > 0, nil
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeMessageRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 可见性（MVP）：三条分支都以 workspace_id 收口——
	//   workspace 广播：本 workspace 成员可见；
	//   actor 私信：仅限本 workspace 内、收发双方（防跨 workspace 读他处私信）；
	//   task thread：只限本 workspace 的任务（子查询约束）。
	query := m.DB.WithContext(r.Context()).Model(&model.Message{}).
		Where("(workspace_id = ? AND target_type = 'workspace') "+
			"OR (workspace_id = ? AND target_type = 'actor' AND (target_id = ? OR sender_id = ?)) "+
			"OR (target_type = 'task' AND target_id IN (SELECT id FROM tasks WHERE workspace_id = ?))",
			wsID, wsID, p.ActorID, p.ActorID, wsID)
	if v := r.URL.Query().Get("thread_id"); v != "" {
		query = query.Where("thread_id = ?", v)
	}
	if v := r.URL.Query().Get("target_id"); v != "" {
		query = query.Where("target_id = ?", v)
	}
	var rows []model.Message
	if err := query.Order("id DESC").Limit(100).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]messageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMessageDTO(row))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}
