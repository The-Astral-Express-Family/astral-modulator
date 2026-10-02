// 工作区邀请生命周期（docs/registration.md / ADR-0009 双轨分离）：
// 签发 / 列表 / 撤销 / **兑换**（POST /invitations/redeem，已登录 human
// 凭码入伙——注册/入伙两轨分离后，这是工作区码唯一的兑换入口）。
// 管理面授权双闸：human session（agent credential 一律 403——程序不能替人
// 决定谁能进来）+ workspace:manage_members（非成员 404 / 不足 403）。
// 兑换面只要 human session：码本身就是授予凭据，持码者即受邀人。
package workspace

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/mail"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
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
	// Email 非空 = 邮件投递的邀请（00024）：链接与站内码同体，撤销/过期/
	// 兑换两渠道同时失效。
	Email       *string `json:"email,omitempty"`
	EmailSentAt *string `json:"email_sent_at,omitempty"`
}

// invitationCreatedDTO 仅用于签发响应：code 明文只出现这一次（库中只有
// hash），invite_url 是拼好的 /join?ws= 链接（面向已注册用户，ADR-0009：
// 工作区码不再是注册入口）。
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
		RedeemedAt: httpx.TimeString(inv.RedeemedAt),
		Email:      inv.Email,
	}
	if inv.EmailSentAt != nil {
		sent := inv.EmailSentAt.UTC().Format(time.RFC3339)
		dto.EmailSentAt = &sent
	}
	return dto
}

// 邀请管理三端点先过 auth.RequireHuman（human session 闸，agent credential
// 一律 403——程序不能替人决定谁能进来），再各自走 requireWorkspace。

func (m *Module) createInvitation(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireHuman(r, "human session required"); apiErr != nil {
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
		Role      string  `json:"role"`
		ExpiresIn *int64  `json:"expires_in"`
		Email     *string `json:"email"`
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
	// email 可选：提供即同时邮件投递（00024）。指针区分「未提供」与空串。
	email := ""
	if in.Email != nil {
		var ok bool
		if email, ok = auth.NormalizeEmail(*in.Email); !ok {
			httpx.WriteError(w, r, httpx.Invalid("email is malformed"))
			return
		}
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
	if email != "" {
		inv.Email = &email
	}
	// 签发与审计/事件同事务；失败则邀请行不落库（明文码随之作废，重签即可）。
	// 同 (workspace, email) 在途邀请在此事务内整体撤销——邮件渠道与站内渠道
	// 同体同生命周期：重签即旧链接立即失效（收件人手上永远只有最新一封）。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if email != "" {
			if err := tx.Model(&model.Invitation{}).
				Where("workspace_id = ? AND email = ? AND status = 'invited'", ws.ID, email).
				Update("status", "revoked").Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		details := map[string]any{"role": in.Role}
		if email != "" {
			details["email"] = email
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "invite.create", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: details,
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
	// 投递在事务外：行已落库，投递失败不回滚（签发响应仍带明文码，管理员可
	// 手动转发）；email_sent_at 标记结果，列表可见，失败可重签（即作废本张）。
	if email != "" {
		inviteURL := m.inviteURL(r, code)
		if err := mail.OrLog(m.Mailer, m.inviteMailLog()).Send(r.Context(), inviteEmail(ws.Name, in.Role, inv.ExpiresAt, email, code, inviteURL)); err != nil {
			m.inviteMailLog().Error("invitation mail delivery failed", "invitation_id", inv.ID, "email", email, "err", err)
		} else {
			inv.EmailSentAt = &now
			if err := m.DB.WithContext(r.Context()).Model(&model.Invitation{}).
				Where("id = ?", inv.ID).Update("email_sent_at", now).Error; err != nil {
				m.inviteMailLog().Warn("invitation email_sent_at write failed", "invitation_id", inv.ID, "err", err)
			}
		}
	}
	httpx.WriteOK(w, r, http.StatusCreated, invitationCreatedDTO{
		invitationDTO: toInvitationDTO(inv),
		Code:          code,
		InviteURL:     m.inviteURL(r, code),
	})
}

func (m *Module) listInvitations(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireHuman(r, "human session required"); apiErr != nil {
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
	if apiErr := auth.RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	invitationID := chi.URLParam(r, "invitation_id")
	var inv model.Invitation
	if apiErr := store.First(m.DB.WithContext(r.Context()), &inv,
		httpx.NotFound("invitation not found"), "id = ?", invitationID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if _, apiErr := m.requireWorkspace(r, inv.WorkspaceID, auth.ScopeWorkspaceManageMember); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 状态转移与审计/事件同一事务（与 invite.create 同规矩）：并发双撤只有
	// 一方抢到 invited→revoked，输家与已关闭（redeemed/revoked）的重复撤销
	// 都幂等 204，不落审计/事件。
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Invitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Update("status", "revoked")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
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

// inviteURL 拼入伙链接 {WebBaseURL}/join?ws=<code>：收件人是已注册用户
// （ADR-0009——工作区码不再指向 /register）；基址回退链与 device
// verification_uri 同源（auth.ResolveWebBaseURL），不再另造拼装点。
func (m *Module) inviteURL(r *http.Request, code string) string {
	base := auth.ResolveWebBaseURL(m.WebBaseURL, m.PublicURL, r)
	return strings.TrimRight(base, "/") + "/join?ws=" + url.QueryEscape(code)
}

// errInviteInvalid 与 auth 包同文案同语义（防探测：查无/已用/撤销/过期/
// 类型不符一律同码同消息）。独立构造，避免 workspace→auth 内部符号耦合。
var errInviteInvalid = &httpx.APIError{
	Status:  http.StatusBadRequest,
	Code:    httpx.CodeInviteInvalid,
	Message: "invite code is invalid or expired",
}

var errAlreadyMember = &httpx.APIError{
	Status:  http.StatusConflict,
	Code:    httpx.CodeAlreadyMember,
	Message: "already a member of this workspace",
}

// redeemedDTO 是 POST /invitations/redeem 的响应：入伙结果（workspace +
// 生效角色）。幂等重试（本人已兑过同码）与首次兑换同形状。
type redeemedDTO struct {
	Workspace workspaceDTO `json:"workspace"`
	Role      string       `json:"role"`
}

// redeemInvitation 是 POST /invitations/redeem {code}：已登录 human 凭
// 工作区邀请码入伙（ADR-0009——工作区码=权限授予，不再出现在注册端点）。
//
// 语义边界：
//   - 查无/已用（非本人）/撤销/过期/注册码填入 → 400 INVITE_INVALID
//     （同码同文案防探测）；
//   - 本人已兑过同码 → 幂等 200（网络重试安全，返回现成员角色）；
//   - 已是该 workspace 成员（另一张有效码）→ 409 ALREADY_MEMBER，
//     码不消耗；
//   - agent credential → 403（入伙是人的行为）。
func (m *Module) redeemInvitation(w http.ResponseWriter, r *http.Request) {
	if apiErr := auth.RequireHuman(r, "human session required"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		Code string `json:"code"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Code) == "" {
		httpx.WriteError(w, r, httpx.Invalid("code is required"))
		return
	}

	var inv model.Invitation
	codeHash := auth.HashToken(auth.NormalizeInviteCode(in.Code))
	err := m.DB.WithContext(r.Context()).Where("code_hash = ?", codeHash).First(&inv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, r, errInviteInvalid)
			return
		}
		httpx.RespondError(w, r, err)
		return
	}

	// 幂等窗口先于失效判定：本人重试已兑过的码必须成功，而非 INVITE_INVALID。
	if inv.Status == "redeemed" && inv.RedeemedBy != nil && *inv.RedeemedBy == p.ActorID {
		var mem model.WorkspaceMember
		if err := m.DB.WithContext(r.Context()).
			First(&mem, "workspace_id = ? AND actor_id = ?", inv.WorkspaceID, p.ActorID).Error; err == nil {
			m.writeRedeemed(w, r, inv.WorkspaceID, mem.Role)
			return
		}
		// 兑过码但成员行已被移除：走下方正常路径（重新入伙需码仍是
		// invited，否则按失效处理）。
	}

	if inv.Status != "invited" || time.Now().After(inv.ExpiresAt) {
		httpx.WriteError(w, r, errInviteInvalid)
		return
	}
	var memberCount int64
	if err := m.DB.WithContext(r.Context()).Model(&model.WorkspaceMember{}).
		Where("workspace_id = ? AND actor_id = ?", inv.WorkspaceID, p.ActorID).
		Count(&memberCount).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	if memberCount > 0 {
		httpx.WriteError(w, r, errAlreadyMember)
		return
	}

	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// 条件更新抢状态（tag confirm 验证过的模式）：并发同码只有一个赢家。
		res := tx.Model(&model.Invitation{}).
			Where("id = ? AND status = 'invited'", inv.ID).
			Updates(map[string]any{"status": "redeemed", "redeemed_by": p.ActorID, "redeemed_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errInviteInvalid
		}
		mem := model.WorkspaceMember{WorkspaceID: inv.WorkspaceID, ActorID: p.ActorID, Role: inv.Role}
		if err := tx.Create(&mem).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: inv.WorkspaceID, ActorID: p.ActorID,
			Action: "invite.redeem", Outcome: "allowed",
			TargetType: "invitation", TargetID: inv.ID,
			Details: map[string]any{"role": inv.Role, "via": "redeem"},
		}); err != nil {
			return err
		}
		if err := outbox.EmitTx(tx, outbox.TypeSecurityInviteRedeemed, inv.WorkspaceID, p.ActorID, 0, map[string]any{
			"invitation_id": inv.ID, "role": inv.Role, "via": "redeem",
		}); err != nil {
			return err
		}
		// 与 addMember 同事件面：SSE 订阅方对「管理员加人」与「兑码入伙」
		// 看到同一 type，change 区分来源。
		return outbox.EmitTx(tx, outbox.TypeWorkspaceMemberChanged, inv.WorkspaceID, p.ActorID, 0, map[string]any{
			"actor_id": p.ActorID, "role": inv.Role, "change": "joined",
		})
	})
	if err != nil {
		if errors.Is(err, errInviteInvalid) {
			httpx.WriteError(w, r, errInviteInvalid)
			return
		}
		// 并发窗口：抢到码但成员行已被 addMember 建立（唯一约束）→
		// 整体回滚（码不消耗），按已是成员告知。
		if store.IsUniqueViolation(err) {
			httpx.WriteError(w, r, errAlreadyMember)
			return
		}
		httpx.RespondError(w, r, err)
		return
	}
	m.writeRedeemed(w, r, inv.WorkspaceID, inv.Role)
}

// writeRedeemed 加载 workspace 并写 200 入伙结果。
func (m *Module) writeRedeemed(w http.ResponseWriter, r *http.Request, workspaceID, role string) {
	var ws model.Workspace
	if apiErr := store.First(m.DB.WithContext(r.Context()), &ws,
		httpx.NotFound("workspace not found"), "id = ?", workspaceID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, redeemedDTO{Workspace: toWorkspaceDTO(ws), Role: role})
}

// inviteRoleNames 是邮件正文里的角色中文名（协议枚举保持英文，仅展示层翻译）。
var inviteRoleNames = map[string]string{
	"viewer":      "只读（viewer）",
	"contributor": "读写（contributor）",
	"maintainer":  "管理（maintainer）",
}

// inviteEmail 组装工作区邀请邮件（00024）。链接与站内码同体：正文同时给
// 链接和明文码，任一渠道兑换都消耗同一张邀请。
func inviteEmail(workspaceName, role string, expiresAt time.Time, to, code, inviteURL string) mail.Message {
	roleName := inviteRoleNames[role]
	if roleName == "" {
		roleName = role
	}
	expiry := expiresAt.Local().Format("2006-01-02 15:04")
	subject := "邀请你加入工作区「" + workspaceName + "」"
	text := fmt.Sprintf(`你被邀请加入 Astral 工作区「%s」，授予角色：%s。

打开链接直接入伙（需已注册 Astral 账号；没有账号请先找管理员注册）：
%s

也可以登录后在「加入工作区」中输入邀请码：
%s

有效期至 %s。邀请被撤销或兑换后，链接与邀请码同时失效。`, workspaceName, roleName, inviteURL, code, expiry)
	html := fmt.Sprintf(`<p>你被邀请加入 Astral 工作区「%s」，授予角色：%s。</p>
<p><a href="%s">点击入伙</a>（需已注册 Astral 账号；没有账号请先找管理员注册）</p>
<p>也可以登录后在「加入工作区」中输入邀请码：<code>%s</code></p>
<p>有效期至 %s。邀请被撤销或兑换后，链接与邀请码同时失效。</p>`,
		workspaceName, roleName, inviteURL, code, expiry)
	return mail.Message{To: to, Subject: subject, Text: text, HTML: html}
}
