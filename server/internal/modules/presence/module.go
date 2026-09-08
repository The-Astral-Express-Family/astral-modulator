// Package presence 模块：短生命周期展示状态（architecture §6.8）。
// Presence 不是任务所有权；offline 由 expires_at 读路径派生。
package presence

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
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
	r.Put("/workspaces/{workspace_id}/presence/me", m.heartbeat)
	r.Get("/workspaces/{workspace_id}/presence", m.list)
}

type presenceDTO struct {
	ActorID         string  `json:"actor_id"`
	DisplayName     string  `json:"display_name,omitempty"`
	State           string  `json:"state"`
	CurrentTaskID   *string `json:"current_task_id"`
	Note            *string `json:"note"`
	LastHeartbeatAt string  `json:"last_heartbeat_at"`
	ExpiresAt       string  `json:"expires_at"`
}

// requireWorkspace：workspace 级端点的授权前置（非成员 404 / scope 不足 403）。
func (m *Module) requireWorkspace(r *http.Request, wsID string, need string) *httpx.APIError {
	p := auth.PrincipalFrom(r.Context())
	_, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, wsID, need)
	return apiErr
}

func (m *Module) heartbeat(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopePresenceWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		State         string  `json:"state"`
		CurrentTaskID *string `json:"current_task_id"`
		Note          *string `json:"note"`
		TTLSeconds    int     `json:"ttl_seconds"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	switch in.State {
	case "idle", "planning", "working", "waiting", "blocked", "reviewing":
	default:
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid state"})
		return
	}
	ttl := time.Duration(in.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	if ttl < m.Auth.PresenceMin {
		ttl = m.Auth.PresenceMin
	}
	if ttl > m.Auth.PresenceMax {
		ttl = m.Auth.PresenceMax // 服务端钳制，防“声明一周在线”（openapi PresenceInput.ttl_seconds 边界）
	}
	now := time.Now()
	row := model.Presence{
		ActorID: p.ActorID, WorkspaceID: wsID, State: in.State,
		CurrentTaskID: in.CurrentTaskID, Note: in.Note,
		LastHeartbeatAt: now, ExpiresAt: now.Add(ttl),
	}
	// upsert（sqlite/PG 双方言：先删后插，串行写安全；TODO(phase-4): ON CONFLICT 化）。
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("actor_id = ?", p.ActorID).Delete(&model.Presence{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeActorPresenceChanged, wsID, p.ActorID, 0, map[string]any{
			"actor_id": p.ActorID, "state": in.State, "current_task_id": in.CurrentTaskID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, presenceDTO{
		ActorID: p.ActorID, State: in.State, CurrentTaskID: in.CurrentTaskID, Note: in.Note,
		LastHeartbeatAt: now.UTC().Format(time.RFC3339), ExpiresAt: row.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeWorkspaceRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var rows []model.Presence
	if err := m.DB.WithContext(r.Context()).Where("workspace_id = ?", wsID).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	now := time.Now()
	items := make([]presenceDTO, 0, len(rows))
	for _, row := range rows {
		state := row.State
		if row.ExpiresAt.Before(now) {
			state = "offline" // 读路径派生（不落库）
		}
		var actor model.Actor
		display := ""
		_ = m.DB.WithContext(r.Context()).Select("display_name").First(&actor, "id = ?", row.ActorID).Error
		display = actor.DisplayName
		items = append(items, presenceDTO{
			ActorID: row.ActorID, DisplayName: display, State: state,
			CurrentTaskID: row.CurrentTaskID, Note: row.Note,
			LastHeartbeatAt: row.LastHeartbeatAt.UTC().Format(time.RFC3339),
			ExpiresAt:       row.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items})
}
