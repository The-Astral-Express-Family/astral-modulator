// Package admin 是平台管理员模块（TODO.md round 34）：全局用户列表、
// 停用/恢复、平台角色变更。授权一律经 auth.RequireGlobal（platform:users:*），
// handler 只做编解码、事务与审计编排；agent/service 账号不在本模块管辖
// （归属随 workspace，D9），这里只管 human。
package admin

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// Module 是 HTTP 层：路由注册 + 请求/响应编解码。
type Module struct {
	DB   *gorm.DB
	Auth *auth.Service
	Log  *slog.Logger
}

// RegisterRoutes 挂载 /admin/* 端点（受保护组内；scope 不足 403 INSUFFICIENT_SCOPE）。
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/admin/users", m.listUsers)
	r.Post("/admin/users/{actor_id}/disable", m.disableUser)
	r.Post("/admin/users/{actor_id}/enable", m.enableUser)
	r.Post("/admin/users/{actor_id}/role", m.changeRole)
}

// UserDTO 是 admin 用户列表项：平台视角比 ActorDTO 多邮箱/角色/停用态。
// 邮箱仅 admin 可见（platform:users:read）。
type UserDTO struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	PlatformRole string  `json:"platform_role"`
	DisplayName  string  `json:"display_name"`
	Email        string  `json:"email,omitempty"`
	DisabledAt   *string `json:"disabled_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func toUserDTO(a model.Actor, email string) UserDTO {
	dto := UserDTO{
		ID:           a.ID,
		Kind:         a.Kind,
		PlatformRole: a.PlatformRole,
		DisplayName:  a.DisplayName,
		Email:        email,
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.DisabledAt != nil {
		disabled := a.DisabledAt.UTC().Format(time.RFC3339)
		dto.DisabledAt = &disabled
	}
	return dto
}

// listUsers 是 GET /admin/users：全量 human 账号（平台用户量小，沿用现有
// 无分页列表惯例；统一分页 envelope 见 TODO.md §11 #11）。
func (m *Module) listUsers(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var actors []model.Actor
	if err := m.DB.WithContext(r.Context()).
		Where("kind = ?", "human").
		Order("created_at ASC").
		Find(&actors).Error; err != nil {
		httpx.WriteError(w, r, httpx.Internal("lookup failed"))
		return
	}
	emails := m.emailsByActor(r, actors)
	items := make([]UserDTO, 0, len(actors))
	for _, a := range actors {
		items = append(items, toUserDTO(a, emails[a.ID]))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

// emailsByActor 批量取 human_auth 邮箱；查失败按缺邮箱处理（列表不因
// 附属信息失败而失败，对齐 meBody 的容错取向）。
func (m *Module) emailsByActor(r *http.Request, actors []model.Actor) map[string]string {
	out := map[string]string{}
	if len(actors) == 0 {
		return out
	}
	ids := make([]string, 0, len(actors))
	for _, a := range actors {
		ids = append(ids, a.ID)
	}
	var rows []model.HumanAuth
	if err := m.DB.WithContext(r.Context()).Where("actor_id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, row := range rows {
		out[row.ActorID] = row.Email
	}
	return out
}

// loadManagedHuman 是变更端点的公共前置：目标必须存在且为 human。
func (m *Module) loadManagedHuman(r *http.Request) (*model.Actor, *httpx.APIError) {
	var target model.Actor
	if apiErr := store.First(m.DB.WithContext(r.Context()), &target,
		httpx.NotFound("user not found"), "id = ?", chi.URLParam(r, "actor_id")); apiErr != nil {
		return nil, apiErr
	}
	if target.Kind != "human" {
		return nil, httpx.Invalid("only human accounts can be managed here")
	}
	return &target, nil
}

func (m *Module) disableUser(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	target, apiErr := m.loadManagedHuman(r)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 防自锁：不允许停用自己（last-admin 竞态 MVP 接受，见 TODO.md round 34）。
	if target.ID == p.ActorID {
		httpx.WriteError(w, r, httpx.Invalid("cannot disable your own account"))
		return
	}
	if target.DisabledAt != nil {
		httpx.WriteError(w, r, httpx.Conflict(httpx.CodeValidationFailed, "account already disabled"))
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Actor{}).Where("id = ? AND disabled_at IS NULL", target.ID).
			Update("disabled_at", time.Now()).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "platform.user.disable", Outcome: "allowed",
			TargetType: "actor", TargetID: target.ID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// 在途会话立即作废并断流；准入闸门是 DisabledAt 检查，此处失败不回滚
	// 停用（残留会话在下一次请求即被 authActor 拒绝），只记日志。
	if _, err := m.Auth.RevokeActorSessions(r.Context(), target.ID); err != nil {
		m.Log.Error("revoke sessions after disable failed", "actor_id", target.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) enableUser(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	target, apiErr := m.loadManagedHuman(r)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if target.DisabledAt == nil {
		httpx.WriteError(w, r, httpx.Conflict(httpx.CodeValidationFailed, "account not disabled"))
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Actor{}).Where("id = ? AND disabled_at IS NOT NULL", target.ID).
			Update("disabled_at", nil).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "platform.user.enable", Outcome: "allowed",
			TargetType: "actor", TargetID: target.ID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) changeRole(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		PlatformRole string `json:"platform_role"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.PlatformRole != "admin" && in.PlatformRole != "user" {
		httpx.WriteError(w, r, httpx.Invalid("platform_role must be admin or user"))
		return
	}
	target, apiErr := m.loadManagedHuman(r)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 防自锁：不改自己的角色（含自降/自升；角色变更必须出自另一个 admin）。
	if target.ID == p.ActorID {
		httpx.WriteError(w, r, httpx.Invalid("cannot change your own platform role"))
		return
	}
	if target.PlatformRole == in.PlatformRole {
		httpx.WriteError(w, r, httpx.Conflict(httpx.CodeValidationFailed, "platform_role unchanged"))
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Actor{}).Where("id = ?", target.ID).
			Update("platform_role", in.PlatformRole).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "platform.user.role_change", Outcome: "allowed",
			TargetType: "actor", TargetID: target.ID,
			Details: map[string]any{"from": target.PlatformRole, "to": in.PlatformRole},
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	var updated model.Actor
	if err := m.DB.WithContext(r.Context()).First(&updated, "id = ?", target.ID).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	email := m.emailsByActor(r, []model.Actor{updated})[updated.ID]
	httpx.WriteOK(w, r, http.StatusOK, toUserDTO(updated, email))
}
