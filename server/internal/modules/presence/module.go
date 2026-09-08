// Package presence 模块：短生命周期展示状态（architecture §6.8）。
// Presence 按 (actor, workspace) 一行：同一 actor 可在多个 workspace 各自
// heartbeat，互不影响。不是任务所有权；offline 由 expires_at 读路径派生。
package presence

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
)

// TTL 钳制边界（openapi PresenceInput.ttl_seconds 的服务端约束）。
var (
	TTLDefault = 90 * time.Second
	TTLMin     = 30 * time.Second
	TTLMax     = 5 * time.Minute
)

type Module struct {
	DB   *gorm.DB
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
		httpx.WriteError(w, r, httpx.Invalid("invalid state"))
		return
	}
	ttl := clampTTL(in.TTLSeconds)
	now := time.Now()
	row := model.Presence{
		ActorID: p.ActorID, WorkspaceID: wsID, State: in.State,
		CurrentTaskID: in.CurrentTaskID, Note: in.Note,
		LastHeartbeatAt: now, ExpiresAt: now.Add(ttl),
	}
	// upsert（sqlite/PG 双方言：先删后插，串行写安全；TODO(phase-6): ON CONFLICT 化）。
	// 删除只限本 workspace：actor 在其它 workspace 的 presence 不受影响。
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("actor_id = ? AND workspace_id = ?", p.ActorID, wsID).Delete(&model.Presence{}).Error; err != nil {
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

// list 按 expires_at 读路径派生 offline；actor 展示名一次 IN 查询取回。
// 已过期的行保留（GUI 需要“刚离开”的展示），派生为 offline。
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
	actorIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		actorIDs = append(actorIDs, row.ActorID)
	}
	names := map[string]string{}
	if len(actorIDs) > 0 {
		var actors []model.Actor
		if err := m.DB.WithContext(r.Context()).Select("id", "display_name").
			Where("id IN ?", actorIDs).Find(&actors).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.RespondError(w, r, err)
			return
		}
		for _, a := range actors {
			names[a.ID] = a.DisplayName
		}
	}
	now := time.Now()
	items := make([]presenceDTO, 0, len(rows))
	for _, row := range rows {
		state := row.State
		if row.ExpiresAt.Before(now) {
			state = "offline" // 读路径派生（不落库）
		}
		items = append(items, presenceDTO{
			ActorID: row.ActorID, DisplayName: names[row.ActorID], State: state,
			CurrentTaskID: row.CurrentTaskID, Note: row.Note,
			LastHeartbeatAt: row.LastHeartbeatAt.UTC().Format(time.RFC3339),
			ExpiresAt:       row.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items})
}

func clampTTL(in int) time.Duration {
	ttl := time.Duration(in) * time.Second
	if ttl <= 0 {
		ttl = TTLDefault
	}
	if ttl < TTLMin {
		ttl = TTLMin
	}
	if ttl > TTLMax {
		ttl = TTLMax // 服务端钳制，防“声明一周在线”
	}
	return ttl
}
