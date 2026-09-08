// Package tag 模块：两步确认的 proposal/confirm（architecture §14）。
// 产品语义：让 Agent “再想一次”，而非自动相似度拒绝。
// 服务端只做确定性规则（trim/NFC/case-fold/精确唯一），语义近似判断交给调用者。
package tag

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
)

const proposalTTL = 120 * time.Second // 只够“三思”，不够挂机

type Module struct {
	DB   *gorm.DB
	Auth *auth.Service
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/tags", m.list)
	r.Post("/workspaces/{workspace_id}/tag-proposals", m.propose)
	r.Post("/tag-proposals/{proposal_id}/confirm", m.confirm)
}

// requireWorkspace：workspace 级端点的授权前置（非成员 404 / scope 不足 403）。
func (m *Module) requireWorkspace(r *http.Request, wsID string, need string) *httpx.APIError {
	p := auth.PrincipalFrom(r.Context())
	_, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, wsID, need)
	return apiErr
}

// ---- DTO ----

type tagDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type proposalDTO struct {
	ProposalID   string   `json:"proposal_id"`
	Action       string   `json:"action"`
	Name         string   `json:"name"`
	ConfirmCode  string   `json:"confirm_code"`
	ExpiresAt    string   `json:"expires_at"`
	ExistingTags []tagDTO `json:"existing_tags"`
}

// ---- handlers ----

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeTagRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var tags []model.Tag
	if err := m.DB.WithContext(r.Context()).
		Where("workspace_id = ?", wsID).Order("created_at ASC").Find(&tags).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]tagDTO, 0, len(tags))
	for _, t := range tags {
		items = append(items, tagDTO{ID: t.ID, Name: t.Name})
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

// propose：两步确认第一步。不创建正式 tag；返回全量 existing_tags 供比对。
func (m *Module) propose(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := m.requireWorkspace(r, wsID, auth.ScopeTagWrite); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		Action      string  `json:"action"`
		Name        string  `json:"name"`
		TargetTagID *string `json:"target_tag_id"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	in.Action = strings.TrimSpace(in.Action)
	if in.Action != "create" && in.Action != "rename" && in.Action != "delete" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed,
			Message: "action must be create|rename|delete"})
		return
	}
	normalized, ok := ValidateTagName(in.Name)
	if !ok {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed,
			Message: "invalid tag name (1-64 chars, no control chars)"})
		return
	}
	if in.Action != "create" {
		if in.TargetTagID == nil || *in.TargetTagID == "" {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed,
				Message: "target_tag_id required for rename/delete"})
			return
		}
		var target model.Tag
		err := m.DB.WithContext(r.Context()).First(&target, "id = ? AND workspace_id = ?", *in.TargetTagID, wsID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, r, httpx.NotFound("target tag not found"))
			return
		}
		if err != nil {
			httpx.RespondError(w, r, err)
			return
		}
	}

	// 确定性预检（同事务内 confirm 时还会复查，防 TOCTOU）：
	// create 目标已存在 → 立即拒绝（这是精确重名，不是模糊相似度判断）。
	if in.Action == "create" {
		var count int64
		if err := m.DB.WithContext(r.Context()).Model(&model.Tag{}).
			Where("workspace_id = ? AND normalized_name = ?", wsID, normalized).Count(&count).Error; err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		if count > 0 {
			httpx.WriteError(w, r, &httpx.APIError{Status: 409, Code: httpx.CodeTagAlreadyExists,
				Message: "tag already exists"})
			return
		}
	}

	code, err := NewConfirmCode()
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	now := time.Now()
	proposal := model.TagProposal{
		ID:              ids.New(ids.TagProposal),
		WorkspaceID:     wsID,
		ActorID:         p.ActorID,
		Action:          in.Action,
		CanonicalName:   normalized,
		TargetTagID:     in.TargetTagID,
		ConfirmCodeHash: auth.HashToken(code),
		Status:          "pending",
		RequestID:       strPtr(r.Header.Get(httpx.HeaderRequestID)),
		ExpiresAt:       now.Add(proposalTTL),
		CreatedAt:       now,
	}
	// 同事务：proposal + audit（proposal 本身即审计-worthy 动作）。
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&proposal).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsID, ActorID: p.ActorID,
			Action: "tag.propose", Outcome: "allowed",
			TargetType: "tag", TargetID: proposal.ID,
			Details: map[string]any{"action": in.Action, "name": normalized},
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}

	// existing_tags 全量返回：Agent 比对后“三思”的数据基础。
	var tags []model.Tag
	if err := m.DB.WithContext(r.Context()).
		Where("workspace_id = ?", wsID).Order("created_at ASC").Find(&tags).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	existing := make([]tagDTO, 0, len(tags))
	for _, t := range tags {
		existing = append(existing, tagDTO{ID: t.ID, Name: t.Name})
	}
	httpx.WriteOK(w, r, http.StatusCreated, proposalDTO{
		ProposalID:   proposal.ID,
		Action:       in.Action,
		Name:         in.Name,
		ConfirmCode:  code,
		ExpiresAt:    proposal.ExpiresAt.UTC().Format(time.RFC3339),
		ExistingTags: existing,
	})
}

// Confirm 两步确认核心（HTTP handler 与测试共用）：
// 单次使用 + 绑定 actor/workspace/action/name + TTL，全部在同一事务内复查；
// tag 唯一约束在事务内二次检查（TOCTOU 兜底）。
func (m *Module) Confirm(ctx context.Context, p *auth.Principal, proposalID, confirmCode, name string) (*model.Tag, error) {
	normalized, ok := ValidateTagName(name)
	if !ok {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid tag name"}
	}

	var tag model.Tag
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var proposal model.TagProposal
		e := tx.First(&proposal, "id = ?", proposalID).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return httpx.NotFound("proposal not found")
		}
		if e != nil {
			return e
		}
		// 绑定校验：actor / workspace / 输入与 canonical 一致 / TTL / 状态。
		if proposal.ActorID != p.ActorID {
			return &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "proposal belongs to another actor"}
		}
		if _, apiErr := m.Auth.RequireWorkspaceScopes(ctx, p, proposal.WorkspaceID, auth.ScopeTagWrite); apiErr != nil {
			return apiErr
		}
		if proposal.Status != "pending" || time.Now().After(proposal.ExpiresAt) {
			return &httpx.APIError{Status: 409, Code: httpx.CodeTagProposalExpired, Message: "proposal expired or already used"}
		}
		if !auth.HashEqual(confirmCode, proposal.ConfirmCodeHash) {
			return &httpx.APIError{Status: 403, Code: httpx.CodeValidationFailed, Message: "confirm_code mismatch"}
		}
		if proposal.CanonicalName != normalized {
			return &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed,
				Message: "name does not match the proposed input"}
		}
		// 单次使用：原子置 confirmed，抢不到即已使用/并发。
		res := tx.Model(&model.TagProposal{}).
			Where("id = ? AND status = 'pending'", proposal.ID).
			Updates(map[string]any{"status": "confirmed", "confirmed_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &httpx.APIError{Status: 409, Code: httpx.CodeTagProposalExpired, Message: "proposal already used"}
		}

		// 执行动作（唯一约束在事务内二次检查，TOCTOU 兜底）。
		var evType string
		switch proposal.Action {
		case "create":
			tag = model.Tag{
				ID: ids.New(ids.Tag), WorkspaceID: proposal.WorkspaceID,
				Name: name, NormalizedName: normalized,
				CreatedBy: p.ActorID, CreatedAt: time.Now(),
			}
			if err := tx.Create(&tag).Error; err != nil {
				if isUniqueViolation(err) {
					return &httpx.APIError{Status: 409, Code: httpx.CodeTagAlreadyExists, Message: "tag already exists"}
				}
				return err
			}
			evType = event.TypeTagCreated
		case "rename":
			res := tx.Model(&model.Tag{}).
				Where("id = ? AND workspace_id = ?", *proposal.TargetTagID, proposal.WorkspaceID).
				Updates(map[string]any{"name": name, "normalized_name": normalized})
			if res.Error != nil {
				if isUniqueViolation(res.Error) {
					return &httpx.APIError{Status: 409, Code: httpx.CodeTagAlreadyExists, Message: "target name already exists"}
				}
				return res.Error
			}
			if res.RowsAffected == 0 {
				return httpx.NotFound("target tag not found")
			}
			tag.ID, tag.Name = *proposal.TargetTagID, name
			evType = event.TypeTagRenamed
		case "delete":
			res := tx.Where("id = ? AND workspace_id = ?", *proposal.TargetTagID, proposal.WorkspaceID).Delete(&model.Tag{})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return httpx.NotFound("target tag not found")
			}
			tag.ID, tag.Name = *proposal.TargetTagID, name
			evType = event.TypeTagDeleted
		default:
			return &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "unknown proposal action"}
		}

		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: proposal.WorkspaceID, ActorID: p.ActorID,
			Action: "tag." + proposal.Action + ".confirm", Outcome: "allowed",
			TargetType: "tag", TargetID: tag.ID,
			Details: map[string]any{"name": normalized},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, evType, proposal.WorkspaceID, p.ActorID, 0,
			map[string]any{"tag_id": tag.ID, "name": normalized, "action": proposal.Action})
	})
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// confirm：两步确认第二步的 HTTP 面（CLI 固定拼写 --confirm）。
func (m *Module) confirm(w http.ResponseWriter, r *http.Request) {
	proposalID := chi.URLParam(r, "proposal_id")
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		ConfirmCode string `json:"confirm_code"`
		Name        string `json:"name"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	tagRow, err := m.Confirm(r.Context(), p, proposalID, in.ConfirmCode, in.Name)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, tagDTO{ID: tagRow.ID, Name: tagRow.Name})
}

func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint") || strings.Contains(msg, "duplicate key")
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
