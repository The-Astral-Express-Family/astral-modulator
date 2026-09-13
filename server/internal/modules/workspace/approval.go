// approval.go：server-side approval 状态机（architecture §22、TODO.md T-ws-6）。
// 高风险动作先建请求（requested），owner 批准后在同一事务内执行业务变更并
// 置 executed。MVP 唯一动作是 membership.promote_owner；后续动作
// （workspace.delete / credential.create_privileged / task.force_release 等）
// 在本状态机上扩展 action，不复用 tag proposal。
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

// ApprovalTTL 决定窗口：过期由读路径惰性置 expired（无清扫器）。
const ApprovalTTL = 72 * time.Hour

// ActionApprovalPromoteOwner 是 MVP 唯一开放的 approval 动作。
const ActionApprovalPromoteOwner = "membership.promote_owner"

type approvalDTO struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Action        string  `json:"action"`
	TargetActorID string  `json:"target_actor_id"`
	Status        string  `json:"status"`
	RequestedBy   string  `json:"requested_by"`
	DecidedBy     *string `json:"decided_by"`
	DecidedAt     *string `json:"decided_at"`
	ExpiresAt     string  `json:"expires_at"`
	CreatedAt     string  `json:"created_at"`
}

func toApprovalDTO(a model.Approval) approvalDTO {
	dto := approvalDTO{
		ID: a.ID, WorkspaceID: a.WorkspaceID, Action: a.Action,
		TargetActorID: a.TargetActorID, Status: a.Status, RequestedBy: a.RequestedBy,
		DecidedBy: a.DecidedBy,
		ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.DecidedAt != nil {
		t := a.DecidedAt.UTC().Format(time.RFC3339)
		dto.DecidedAt = &t
	}
	return dto
}

func (m *Module) registerApprovalRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/approvals", m.listApprovals)
	r.Post("/workspaces/{workspace_id}/approvals", m.createApproval)
	r.Post("/approvals/{approval_id}/approve", m.decideApproval("approved"))
	r.Post("/approvals/{approval_id}/deny", m.decideApproval("rejected"))
}

// createApproval 建高风险动作请求。发起者需 workspace:manage_members
// （与执行所需权限同级别；approval 的意义在于显式生命周期 + 审计，而非提权）。
func (m *Module) createApproval(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if _, apiErr := m.requireWorkspace(r, wsID, auth.ScopeWorkspaceManageMember); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		Action        string `json:"action"`
		TargetActorID string `json:"target_actor_id"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Action != ActionApprovalPromoteOwner {
		httpx.WriteError(w, r, httpx.Invalid("unknown action (only membership.promote_owner is open)"))
		return
	}

	// 目标必须是本 workspace 的非 owner 成员。
	var member model.WorkspaceMember
	err := m.DB.WithContext(r.Context()).First(&member,
		"workspace_id = ? AND actor_id = ?", wsID, in.TargetActorID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.WriteError(w, r, httpx.NotFound("target actor is not a member of this workspace"))
		return
	}
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	if member.Role == "owner" {
		httpx.WriteError(w, r, httpx.Invalid("target actor is already an owner"))
		return
	}
	var pending int64
	if err := m.DB.WithContext(r.Context()).Model(&model.Approval{}).
		Where("workspace_id = ? AND action = ? AND target_actor_id = ? AND status = 'requested'",
			wsID, in.Action, in.TargetActorID).Count(&pending).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	if pending > 0 {
		httpx.WriteError(w, r, httpx.Conflict(httpx.CodeValidationFailed,
			"pending approval already exists for this target"))
		return
	}

	payload, _ := json.Marshal(map[string]any{"role": "owner"})
	row := model.Approval{
		ID: ids.New(ids.Approval), WorkspaceID: wsID, Action: in.Action,
		TargetActorID: in.TargetActorID, Payload: payload,
		Status: "requested", RequestedBy: p.ActorID,
		ExpiresAt: time.Now().Add(ApprovalTTL),
	}
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "approval.request", Outcome: "allowed",
			TargetType: "approval", TargetID: row.ID,
			Details: map[string]any{"approval_action": in.Action, "target_actor_id": in.TargetActorID},
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, toApprovalDTO(row))
}

func (m *Module) listApprovals(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if _, apiErr := m.requireWorkspace(r, wsID, auth.ScopeWorkspaceManageMember); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	query := m.DB.WithContext(r.Context()).Where("workspace_id = ?", wsID)
	if v := r.URL.Query().Get("status"); v != "" {
		query = query.Where("status = ?", v)
	}
	var rows []model.Approval
	if err := query.Order("created_at DESC").Limit(200).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]approvalDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toApprovalDTO(row))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

// decideApproval 返回 approve/deny 共用的 handler。approve 分支在同一事务内
// 执行 promote（requested -> approved -> executed；执行失败整体回滚）。
// 审批人必须是 workspace owner（role 判断，scope 不够——maintainer 也持有
// manage_members 但不能裁决）。MVP 允许发起人自批（单 owner workspace 需要；
// 双人裁决作为后续收紧项，见 TODO.md）。
func (m *Module) decideApproval(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		approvalID := chi.URLParam(r, "approval_id")
		p := auth.PrincipalFrom(r.Context())

		var row model.Approval
		err := m.DB.WithContext(r.Context()).First(&row, "id = ?", approvalID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, r, httpx.NotFound("approval not found"))
			return
		}
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		if _, apiErr := m.requireWorkspace(r, row.WorkspaceID, auth.ScopeWorkspaceManageMember); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
			return
		}
		if !m.isOwner(r.Context(), row.WorkspaceID, p.ActorID) {
			httpx.WriteError(w, r, httpx.Forbidden("only an owner can decide approvals"))
			return
		}

		now := time.Now()
		err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&model.Approval{}).
				Where("id = ? AND status = 'requested' AND expires_at > ?", row.ID, now).
				Updates(map[string]any{
					"status": decision, "decided_by": p.ActorID, "decided_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return httpx.Conflict(httpx.CodeApprovalExpired, "approval expired or already decided")
			}
			row.Status, row.DecidedBy, row.DecidedAt = decision, &p.ActorID, &now

			if decision != "approved" {
				return audit.RecordInTx(tx, audit.Entry{
					WorkspaceID: row.WorkspaceID, ActorID: p.ActorID,
					Action: "approval.deny", Outcome: "allowed",
					TargetType: "approval", TargetID: row.ID,
					Details: map[string]any{"target_actor_id": row.TargetActorID},
				})
			}
			// approve：业务执行同事务（promote）。
			res = tx.Model(&model.WorkspaceMember{}).
				Where("workspace_id = ? AND actor_id = ?", row.WorkspaceID, row.TargetActorID).
				Update("role", "owner")
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return httpx.NotFound("target actor is no longer a member")
			}
			res = tx.Model(&model.Approval{}).Where("id = ?", row.ID).Update("status", "executed")
			if res.Error != nil {
				return res.Error
			}
			row.Status = "executed"
			if err := audit.RecordInTx(tx, audit.Entry{
				WorkspaceID: row.WorkspaceID, ActorID: p.ActorID,
				Action: "approval.approve", Outcome: "allowed",
				TargetType: "approval", TargetID: row.ID,
				Details: map[string]any{"target_actor_id": row.TargetActorID},
			}); err != nil {
				return err
			}
			return outbox.EmitTx(tx, outbox.TypeWorkspaceMemberChanged, row.WorkspaceID, p.ActorID, 0, map[string]any{
				"actor_id": row.TargetActorID, "role": "owner", "change": "promoted",
				"approval_id": row.ID,
			})
		})
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		httpx.WriteOK(w, r, http.StatusOK, toApprovalDTO(row))
	}
}

func (m *Module) isOwner(ctx context.Context, wsID, actorID string) bool {
	var member model.WorkspaceMember
	err := m.DB.WithContext(ctx).Select("role").First(&member,
		"workspace_id = ? AND actor_id = ?", wsID, actorID).Error
	return err == nil && member.Role == "owner"
}
