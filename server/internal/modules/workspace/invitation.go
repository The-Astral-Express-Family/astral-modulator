// 邀请码生命周期（docs/registration.md，TODO.md A5）：签发 / 列表 / 撤销。
// 兑换在 auth（POST /auth/register 的 invite_code 分支），本文件只管管理面。
// 授权双闸：human session（agent credential 一律 403——程序不能替人决定
// 谁能进来）+ workspace:manage_members（非成员 404 / 不足 403）。
package workspace

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// invitationRoles 是邀请可授角色白名单：永不含 owner（晋升必须走
// membership.promote_owner approval，D8/registration.md §6.3）。
var invitationRoles = map[string]bool{"viewer": true, "contributor": true, "maintainer": true}

// 邀请 TTL：默认 7d，签发时可指定，上限 30d（expires_in 单位秒）。
const (
	defaultInviteTTL = 7 * 24 * time.Hour
	maxInviteTTL     = 30 * 24 * time.Hour
)

type invitationDTO struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	CreatedBy   string  `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	RedeemedBy  *string `json:"redeemed_by,omitempty"`
	RedeemedAt  *string `json:"redeemed_at,omitempty"`
}

// invitationCreatedDTO 仅用于签发响应：code 明文只出现这一次（库中只有
// hash），invite_url 是拼好的 /register?code= 链接，收件人可直接打开。
type invitationCreatedDTO struct {
	invitationDTO
	Code      string `json:"code"`
	InviteURL string `json:"invite_url"`
}

func toInvitationDTO(inv model.Invitation) invitationDTO {
	dto := invitationDTO{
		ID: inv.ID, WorkspaceID: inv.WorkspaceID, Role: inv.Role,
		Status: inv.Status, CreatedBy: inv.CreatedBy,
		CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:  inv.ExpiresAt.UTC().Format(time.RFC3339),
		RedeemedBy: inv.RedeemedBy,
	}
	if inv.RedeemedAt != nil {
		t := inv.RedeemedAt.UTC().Format(time.RFC3339)
		dto.RedeemedAt = &t
	}
	return dto
}

// requireInviteAdmin 是三个管理端点的共同前置：human session + manage_members。
func requireInviteAdmin(r *http.Request) *httpx.APIError {
	if p := auth.PrincipalFrom(r.Context()); p == nil || !p.IsHuman() {
		return httpx.Forbidden("human session required")
	}
	return nil
}

func (m *Module) createInvitation(w http.ResponseWriter, r *http.Request) {
	if apiErr := requireInviteAdmin(r); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		Role      string `json:"role"`
		ExpiresIn *int64 `json:"expires_in"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if !invitationRoles[in.Role] {
		httpx.WriteError(w, r, httpx.Invalid("role must be viewer, contributor or maintainer"))
		return
	}
	ttl := defaultInviteTTL
	if in.ExpiresIn != nil {
		if *in.ExpiresIn <= 0 || *in.ExpiresIn > int64(maxInviteTTL/time.Second) {
			httpx.WriteError(w, r, httpx.Invalid("expires_in must be between 1 and 2592000 seconds"))
			return
		}
		ttl = time.Duration(*in.ExpiresIn) * time.Second
	}
	code, err := auth.NewInviteCode()
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	now := time.Now()
	inv := model.Invitation{
		ID:          ids.New(ids.Invite),
		WorkspaceID: ws.ID,
		Role:        in.Role,
		CodeHash:    auth.HashToken(auth.NormalizeInviteCode(code)),
		CreatedBy:   p.ActorID,
		Status:      "invited",
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	// 签发与审计/事件同事务；失败则邀请行不落库（明文码随之作废，重签即可）。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "invite.create", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"role": in.Role},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteCreated, ws.ID, p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "role": in.Role, "expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339),
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, invitationCreatedDTO{
		invitationDTO: toInvitationDTO(inv),
		Code:          code,
		InviteURL:     m.inviteURL(r, code),
	})
}

func (m *Module) listInvitations(w http.ResponseWriter, r *http.Request) {
	if apiErr := requireInviteAdmin(r); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	wsID := chi.URLParam(r, "workspace_id")
	if _, apiErr := m.requireWorkspace(r, wsID, auth.ScopeWorkspaceManageMember); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	query := m.DB.WithContext(r.Context()).Model(&model.Invitation{}).Where("workspace_id = ?", wsID)
	if s := r.URL.Query().Get("status"); s != "" {
		switch s {
		case "invited", "redeemed", "revoked":
			query = query.Where("status = ?", s)
		default:
			httpx.WriteError(w, r, httpx.Invalid("status must be invited, redeemed or revoked"))
			return
		}
	}
	var rows []model.Invitation
	if err := query.Order("created_at DESC").Limit(200).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]invitationDTO, 0, len(rows))
	for _, inv := range rows {
		items = append(items, toInvitationDTO(inv))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

func (m *Module) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if apiErr := requireInviteAdmin(r); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	invitationID := chi.URLParam(r, "invitation_id")
	var inv model.Invitation
	err := m.DB.WithContext(r.Context()).First(&inv, "id = ?", invitationID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			httpx.WriteError(w, r, httpx.NotFound("invitation not found"))
		} else {
			httpx.RespondError(w, r, err)
		}
		return
	}
	if _, apiErr := m.requireWorkspace(r, inv.WorkspaceID, auth.ScopeWorkspaceManageMember); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 对已关闭（redeemed/revoked）的撤销幂等 204；条件更新兜住并发双撤。
	res := m.DB.WithContext(r.Context()).Model(&model.Invitation{}).
		Where("id = ? AND status = 'invited'", inv.ID).
		Update("status", "revoked")
	if res.Error != nil {
		httpx.RespondError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: inv.WorkspaceID, ActorID: p.ActorID,
			Action: "invite.revoke", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"role": inv.Role},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteRevoked, inv.WorkspaceID, p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inviteURL 拼注册链接 {WebBaseURL}/register?code=<code>；基址回退链与
// device verification_uri 同源（auth.ResolveWebBaseURL），不再另造拼装点。
func (m *Module) inviteURL(r *http.Request, code string) string {
	base := auth.ResolveWebBaseURL(m.WebBaseURL, m.PublicURL, r)
	return strings.TrimRight(base, "/") + "/register?code=" + url.QueryEscape(code)
}
