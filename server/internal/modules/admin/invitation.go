// 注册邀请（00018 落地，00019 更名 platform→registration，ADR-0009）：
// 平台管理员经用户管理页签发，是唯一的注册资格来源——任何账号创建都
// 消耗恰好一张注册邀请码。与 workspace 邀请（workspace/invitation.go）
// 是两条独立轨道：注册邀请无 workspace/role 维度，不产生任何入伙资格。
// 授权经 auth.RequireGlobal（platform:users:manage）；redeemed/expired
// 派生语义与 workspace 邀请一致。
package admin

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
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// 注册邀请 TTL 与 workspace 邀请同规：默认 7d，上限 30d。
const (
	registrationInviteDefaultTTL = 7 * 24 * time.Hour
	registrationInviteMaxTTL     = 30 * 24 * time.Hour
)

type registrationInvitationDTO struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
	RedeemedBy *string `json:"redeemed_by,omitempty"`
	RedeemedAt *string `json:"redeemed_at,omitempty"`
}

// registrationInvitationCreatedDTO 仅用于签发响应：code 明文只出现这一次，
// invite_url 是拼好的 /register?code= 链接（与 workspace 邀请同构）。
type registrationInvitationCreatedDTO struct {
	registrationInvitationDTO
	Code      string `json:"code"`
	InviteURL string `json:"invite_url"`
}

func toRegistrationInvitationDTO(inv model.RegistrationInvitation) registrationInvitationDTO {
	return registrationInvitationDTO{
		ID: inv.ID, Status: inv.Status, CreatedBy: inv.CreatedBy,
		CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:  inv.ExpiresAt.UTC().Format(time.RFC3339),
		RedeemedBy: inv.RedeemedBy,
		RedeemedAt: httpx.TimeString(inv.RedeemedAt),
	}
}

// createRegistrationInvitation 是 POST /admin/registration-invitations：
// 签发注册邀请（唯一注册资格来源）。
func (m *Module) createRegistrationInvitation(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		ExpiresIn *int64 `json:"expires_in"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	ttl := registrationInviteDefaultTTL
	if in.ExpiresIn != nil {
		if *in.ExpiresIn <= 0 || *in.ExpiresIn > int64(registrationInviteMaxTTL/time.Second) {
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
	inv := model.RegistrationInvitation{
		ID:        ids.New(ids.RegistrationInvite),
		CodeHash:  auth.HashToken(auth.NormalizeInviteCode(code)),
		CreatedBy: p.ActorID,
		Status:    "invited",
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	// 签发与审计/事件同事务（与 workspace invite.create 同规矩）：
	// 失败则邀请行不落库。注册邀请无 workspace：workspace_id 留空。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "registration.invite.create", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"ttl_seconds": int64(ttl / time.Second)},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteCreated, "", p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "scope": "registration", "expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339),
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, registrationInvitationCreatedDTO{
		registrationInvitationDTO: toRegistrationInvitationDTO(inv),
		Code:                      code,
		InviteURL:                 m.inviteURL(r, code),
	})
}

// listRegistrationInvitations 是 GET /admin/registration-invitations：
// id 降序游标分页（audit.ListEntries 同惯例），status 过滤可选。
func (m *Module) listRegistrationInvitations(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	query := m.DB.WithContext(r.Context()).Model(&model.RegistrationInvitation{})
	if s := r.URL.Query().Get("status"); s != "" {
		switch s {
		case "invited", "redeemed", "revoked":
			query = query.Where("status = ?", s)
		default:
			httpx.WriteError(w, r, httpx.Invalid("status must be invited, redeemed or revoked"))
			return
		}
	}
	limit := httpx.ParseLimit(r.URL.Query().Get("limit"), 50, 200)
	// 游标是「上一页最后一行的 id」：id 是 uuidv7（前缀 inv_/reg_ 不参与
	// 跨表比较，本表内字典序即时间序）。
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		query = query.Where("id < ?", cursor)
	}
	var rows []model.RegistrationInvitation
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	items := make([]registrationInvitationDTO, 0, len(rows))
	for _, inv := range rows {
		items = append(items, toRegistrationInvitationDTO(inv))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}

// revokeRegistrationInvitation 是 POST /admin/registration-invitations/
// {invitation_id}/revoke：条件更新抢状态，输家幂等 204（与 workspace
// revoke 同构）。
func (m *Module) revokeRegistrationInvitation(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var inv model.RegistrationInvitation
	if apiErr := store.First(m.DB.WithContext(r.Context()), &inv,
		httpx.NotFound("invitation not found"), "id = ?", chi.URLParam(r, "invitation_id")); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.RegistrationInvitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Update("status", "revoked")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "registration.invite.revoke", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteRevoked, "", p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "scope": "registration",
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inviteURL 拼注册链接 {WebBaseURL}/register?code=<code>；基址回退链与
// workspace 邀请 / device verification_uri 同源（auth.ResolveWebBaseURL）。
func (m *Module) inviteURL(r *http.Request, code string) string {
	base := auth.ResolveWebBaseURL(m.WebBaseURL, m.PublicURL, r)
	return strings.TrimRight(base, "/") + "/register?code=" + url.QueryEscape(code)
}
