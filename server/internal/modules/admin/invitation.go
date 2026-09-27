// 平台级注册邀请（00018）：用户管理页签发入口。与 workspace 邀请
// （workspace/invitation.go）互补——平台邀请只管「允许注册」，兑换后是
// 普通 user，不自动入任何 workspace。授权经 auth.RequireGlobal
// （platform:users:manage，与停用/角色变更同款）； redeemed/expired 派生
// 语义与 workspace 邀请一致。
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

// 平台邀请 TTL 与 workspace 邀请同规：默认 7d，上限 30d。
const (
	platformInviteDefaultTTL = 7 * 24 * time.Hour
	platformInviteMaxTTL     = 30 * 24 * time.Hour
)

type platformInvitationDTO struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
	RedeemedBy *string `json:"redeemed_by,omitempty"`
	RedeemedAt *string `json:"redeemed_at,omitempty"`
}

// platformInvitationCreatedDTO 仅用于签发响应：code 明文只出现这一次，
// invite_url 是拼好的 /register?code= 链接（与 workspace 邀请同构）。
type platformInvitationCreatedDTO struct {
	platformInvitationDTO
	Code      string `json:"code"`
	InviteURL string `json:"invite_url"`
}

func toPlatformInvitationDTO(inv model.PlatformInvitation) platformInvitationDTO {
	return platformInvitationDTO{
		ID: inv.ID, Status: inv.Status, CreatedBy: inv.CreatedBy,
		CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:  inv.ExpiresAt.UTC().Format(time.RFC3339),
		RedeemedBy: inv.RedeemedBy,
		RedeemedAt: httpx.TimeString(inv.RedeemedAt),
	}
}

// createPlatformInvitation 是 POST /admin/invitations：签发平台级注册邀请。
func (m *Module) createPlatformInvitation(w http.ResponseWriter, r *http.Request) {
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
	ttl := platformInviteDefaultTTL
	if in.ExpiresIn != nil {
		if *in.ExpiresIn <= 0 || *in.ExpiresIn > int64(platformInviteMaxTTL/time.Second) {
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
	inv := model.PlatformInvitation{
		ID:        ids.New(ids.Invite),
		CodeHash:  auth.HashToken(auth.NormalizeInviteCode(code)),
		CreatedBy: p.ActorID,
		Status:    "invited",
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	// 签发与审计/事件同事务（与 workspace invite.create 同规矩）：
	// 失败则邀请行不落库。平台级：workspace_id 留空。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "platform.invite.create", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"ttl_seconds": int64(ttl / time.Second)},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteCreated, "", p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "scope": "platform", "expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339),
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, platformInvitationCreatedDTO{
		platformInvitationDTO: toPlatformInvitationDTO(inv),
		Code:                  code,
		InviteURL:             m.inviteURL(r, code),
	})
}

// listPlatformInvitations 是 GET /admin/invitations：id 降序游标分页
// （audit.ListEntries 同惯例），status 过滤可选。
func (m *Module) listPlatformInvitations(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	query := m.DB.WithContext(r.Context()).Model(&model.PlatformInvitation{})
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
	// 游标是「上一页最后一行的 id」：id 前缀同为 inv_ + uuidv7，字典序即时间序。
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		query = query.Where("id < ?", cursor)
	}
	var rows []model.PlatformInvitation
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	items := make([]platformInvitationDTO, 0, len(rows))
	for _, inv := range rows {
		items = append(items, toPlatformInvitationDTO(inv))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}

// revokePlatformInvitation 是 POST /admin/invitations/{invitation_id}/revoke：
// 条件更新抢状态，输家幂等 204（与 workspace revoke 同构）。
func (m *Module) revokePlatformInvitation(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireGlobal(r, auth.ScopePlatformUsersManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var inv model.PlatformInvitation
	if apiErr := store.First(m.DB.WithContext(r.Context()), &inv,
		httpx.NotFound("invitation not found"), "id = ?", chi.URLParam(r, "invitation_id")); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.PlatformInvitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Update("status", "revoked")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			ActorID: p.ActorID, Action: "platform.invite.revoke", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeSecurityInviteRevoked, "", p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "scope": "platform",
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
