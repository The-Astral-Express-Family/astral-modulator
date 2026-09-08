// Package message 模块：Actor 私信 / workspace 广播 / task thread（architecture §6.7）。
package message

import (
	"encoding/json"
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
	Hub  *event.Hub
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

func (m *Module) send(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	scopes, err := m.Auth.WorkspaceScopes(r.Context(), p, wsID)
	if err != nil || len(scopes) == 0 {
		httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeWorkspaceNotFound, Message: "workspace not found"})
		return
	}
	if apiErr := auth.HasScope(scopes, auth.ScopeMessageSend); apiErr != nil {
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
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "body required (1-20000 chars)"})
		return
	}
	if in.Target.ID == "" {
		// 缺省 workspace 广播。
		in.Target.Type = "workspace"
		in.Target.ID = wsID
	}
	switch in.Target.Type {
	case "actor":
		var actor model.Actor
		if err := m.DB.WithContext(r.Context()).First(&actor, "id = ?", in.Target.ID).Error; err != nil {
			httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "target actor not found"})
			return
		}
	case "workspace":
		in.Target.ID = wsID
	case "task":
		var t model.Task
		if err := m.DB.WithContext(r.Context()).First(&t, "id = ?", in.Target.ID).Error; err != nil || t.WorkspaceID != wsID {
			httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeTaskNotFound, Message: "target task not found in workspace"})
			return
		}
	default:
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "target.type must be actor|workspace|task"})
		return
	}
	var thread *string
	if in.ThreadID != nil && *in.ThreadID != "" {
		var parent model.Message
		if err := m.DB.WithContext(r.Context()).First(&parent, "id = ?", *in.ThreadID).Error; err != nil {
			httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "thread_id not found"})
			return
		}
		thread = in.ThreadID
	}
	meta, _ := json.Marshal(in.Metadata)

	row := model.Message{
		ID: ids.New(ids.Message), WorkspaceID: wsID, ThreadID: thread,
		TargetType: in.Target.Type, TargetID: in.Target.ID,
		SenderID: p.ActorID, Body: in.Body, Metadata: meta,
		CreatedAt: time.Now(),
	}
	if err := m.DB.WithContext(r.Context()).Create(&row).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	if m.Hub != nil {
		m.Hub.Publish(event.Envelope{
			ID: ids.New(ids.Event), Type: event.TypeMessageCreated,
			WorkspaceID: wsID, ActorID: p.ActorID,
			OccurredAt: row.CreatedAt.UTC().Format(time.RFC3339), SchemaVersion: 1,
			Data: map[string]any{"message_id": row.ID, "target_type": row.TargetType, "target_id": row.TargetID},
		})
	}
	httpx.WriteOK(w, r, http.StatusCreated, messageDTO{
		ID: row.ID, WorkspaceID: row.WorkspaceID, ThreadID: row.ThreadID,
		SenderID: row.SenderID, TargetType: row.TargetType, TargetID: row.TargetID,
		Body: row.Body, Metadata: json.RawMessage(row.Metadata),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	scopes, err := m.Auth.WorkspaceScopes(r.Context(), p, wsID)
	if err != nil || len(scopes) == 0 {
		httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeWorkspaceNotFound, Message: "workspace not found"})
		return
	}
	if apiErr := auth.HasScope(scopes, auth.ScopeMessageRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 可见性（MVP）：workspace 广播对本 workspace 成员可见；actor 私信仅收发双方；
	// task thread 消息只限本 workspace 的任务（子查询约束，防跨 workspace 读）。
	query := m.DB.WithContext(r.Context()).Model(&model.Message{}).
		Where("(workspace_id = ? AND target_type = 'workspace') "+
			"OR (target_type = 'actor' AND (target_id = ? OR sender_id = ?)) "+
			"OR (target_type = 'task' AND target_id IN (SELECT id FROM tasks WHERE workspace_id = ?))",
			wsID, p.ActorID, p.ActorID, wsID)
	if v := r.URL.Query().Get("thread_id"); v != "" {
		query = query.Where("thread_id = ?", v)
	}
	if v := r.URL.Query().Get("target_id"); v != "" {
		query = query.Where("target_id = ?", v)
	}
	var rows []model.Message
	if err := query.Order("created_at DESC, id DESC").Limit(100).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]messageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, messageDTO{
			ID: row.ID, WorkspaceID: row.WorkspaceID, ThreadID: row.ThreadID,
			SenderID: row.SenderID, TargetType: row.TargetType, TargetID: row.TargetID,
			Body: row.Body, Metadata: json.RawMessage(row.Metadata),
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}
