// Package tag 模块：两步确认的 proposal/confirm（architecture §14）。
// 产品语义：让 Agent “再想一次”，而非自动相似度拒绝。
// 服务端只做确定性规则（trim/NFC/case-fold/精确唯一），语义近似判断交给调用者。
package tag

import (
	"context"
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
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ptr"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
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

// ---- DTO ----

// TagDTO 是 tag 的公网形状（openapi Tag schema：id/workspace_id/name）。
// task 模块的任务 tags 字段复用本类型，避免两份手写 DTO 漂移。
type TagDTO struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

type tagDTO = TagDTO

type proposalDTO struct {
	ProposalID   string   `json:"proposal_id"`
	Action       string   `json:"action"`
	Name         string   `json:"name"`
	ConfirmCode  string   `json:"confirm_code"`
	ExpiresAt    string   `json:"expires_at"`
	ExistingTags []tagDTO `json:"existing_tags"`
}

// ---- handlers ----

// workspaceTags 取 workspace 全量 tag 并组装 DTO（list 端点与 propose 的
// existing_tags 共用——查询、排序与行组装只有这一处实现）。
func (m *Module) workspaceTags(ctx context.Context, wsID string) ([]tagDTO, error) {
	var tags []model.Tag
	if err := m.DB.WithContext(ctx).
		Where("workspace_id = ?", wsID).Order("created_at ASC").Find(&tags).Error; err != nil {
		return nil, err
	}
	items := make([]tagDTO, 0, len(tags))
	for _, t := range tags {
		items = append(items, tagDTO{ID: t.ID, WorkspaceID: t.WorkspaceID, Name: t.Name})
	}
	return items, nil
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTagRead); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	items, err := m.workspaceTags(r.Context(), wsID)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

// propose：两步确认第一步。不创建正式 tag；返回全量 existing_tags 供比对。
func (m *Module) propose(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	p := auth.PrincipalFrom(r.Context())
	if apiErr := auth.RequireWorkspace(r, m.Auth, wsID, auth.ScopeTagWrite); apiErr != nil {
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
		httpx.WriteError(w, r, httpx.Invalid("action must be create|rename|delete"))
		return
	}
	normalized, ok := ValidateTagName(in.Name)
	if !ok {
		httpx.WriteError(w, r, httpx.Invalid("invalid tag name (1-64 chars, no control chars)"))
		return
	}
	if in.Action != "create" {
		if in.TargetTagID == nil || *in.TargetTagID == "" {
			httpx.WriteError(w, r, httpx.Invalid("target_tag_id required for rename/delete"))
			return
		}
		var target model.Tag
		if apiErr := store.First(m.DB.WithContext(r.Context()), &target,
			httpx.NotFound("target tag not found"),
			"id = ? AND workspace_id = ?", *in.TargetTagID, wsID); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
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
			httpx.WriteError(w, r, httpx.Conflict(httpx.CodeTagAlreadyExists, "tag already exists"))
			return
		}
	}

	code, err := NewConfirmCode()
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// 空请求头保持 NULL 语义（原 strPtr 行为），不写空串。
	var reqID *string
	if v := r.Header.Get(httpx.HeaderRequestID); v != "" {
		reqID = ptr.Of(v)
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
		RequestID:       reqID,
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
	existing, err := m.workspaceTags(r.Context(), wsID)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
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
		return nil, httpx.Invalid("invalid tag name")
	}

	var tag model.Tag
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		proposal, err := loadProposalTx(tx, proposalID)
		if err != nil {
			return err
		}
		if err := m.checkProposal(ctx, p, &proposal, confirmCode, normalized); err != nil {
			return err
		}
		if err := claimProposalTx(tx, proposal.ID); err != nil {
			return err
		}
		newTag, evType, err := applyTagAction(tx, p.ActorID, &proposal, name, normalized)
		if err != nil {
			return err
		}
		tag = newTag
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: proposal.WorkspaceID, ActorID: p.ActorID,
			Action: "tag." + proposal.Action + ".confirm", Outcome: "allowed",
			TargetType: "tag", TargetID: tag.ID,
			Details: map[string]any{"name": normalized},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, evType, proposal.WorkspaceID, p.ActorID, 0,
			map[string]any{"tag_id": tag.ID, "name": normalized, "action": proposal.Action})
	})
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// loadProposalTx 事务内加载 proposal 行。
func loadProposalTx(tx *gorm.DB, proposalID string) (model.TagProposal, error) {
	var proposal model.TagProposal
	if apiErr := store.First(tx, &proposal, httpx.NotFound("proposal not found"), "id = ?", proposalID); apiErr != nil {
		return proposal, apiErr
	}
	return proposal, nil
}

// checkProposal 校验 proposal 与本次 confirm 的绑定关系：
// actor / workspace scope / 状态与 TTL / confirm code / canonical name 一致。
func (m *Module) checkProposal(ctx context.Context, p *auth.Principal, proposal *model.TagProposal, confirmCode, normalized string) error {
	if proposal.ActorID != p.ActorID {
		return httpx.Forbidden("proposal belongs to another actor")
	}
	if _, apiErr := m.Auth.RequireWorkspaceScopes(ctx, p, proposal.WorkspaceID, auth.ScopeTagWrite); apiErr != nil {
		return apiErr
	}
	if proposal.Status != "pending" || time.Now().After(proposal.ExpiresAt) {
		return httpx.Conflict(httpx.CodeTagProposalExpired, "proposal expired or already used")
	}
	if !auth.HashEqual(confirmCode, proposal.ConfirmCodeHash) {
		return &httpx.APIError{Status: 403, Code: httpx.CodeValidationFailed, Message: "confirm_code mismatch"}
	}
	if proposal.CanonicalName != normalized {
		return httpx.Invalid("name does not match the proposed input")
	}
	return nil
}

// claimProposalTx 单次使用：原子置 confirmed，抢不到即已使用/并发。
func claimProposalTx(tx *gorm.DB, proposalID string) error {
	res := tx.Model(&model.TagProposal{}).
		Where("id = ? AND status = 'pending'", proposalID).
		Updates(map[string]any{"status": "confirmed", "confirmed_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return httpx.Conflict(httpx.CodeTagProposalExpired, "proposal already used")
	}
	return nil
}

// applyTagAction 执行 proposal 动作（create/rename/delete），返回受影响的
// tag 与对应领域事件类型。唯一约束冲突在事务内二次检查（TOCTOU 兜底）。
func applyTagAction(tx *gorm.DB, actorID string, proposal *model.TagProposal, name, normalized string) (model.Tag, string, error) {
	switch proposal.Action {
	case "create":
		return applyTagCreate(tx, actorID, proposal, name, normalized)
	case "rename":
		return applyTagRename(tx, proposal, name, normalized)
	case "delete":
		return applyTagDelete(tx, proposal, name)
	default:
		return model.Tag{}, "", httpx.Internal("unknown proposal action")
	}
}

func applyTagCreate(tx *gorm.DB, actorID string, proposal *model.TagProposal, name, normalized string) (model.Tag, string, error) {
	tag := model.Tag{
		ID: ids.New(ids.Tag), WorkspaceID: proposal.WorkspaceID,
		Name: name, NormalizedName: normalized,
		CreatedBy: actorID, CreatedAt: time.Now(),
	}
	if err := tx.Create(&tag).Error; err != nil {
		if store.IsUniqueViolation(err) {
			return tag, "", httpx.Conflict(httpx.CodeTagAlreadyExists, "tag already exists")
		}
		return tag, "", err
	}
	return tag, outbox.TypeTagCreated, nil
}

func applyTagRename(tx *gorm.DB, proposal *model.TagProposal, name, normalized string) (model.Tag, string, error) {
	res := tx.Model(&model.Tag{}).
		Where("id = ? AND workspace_id = ?", *proposal.TargetTagID, proposal.WorkspaceID).
		Updates(map[string]any{"name": name, "normalized_name": normalized})
	if res.Error != nil {
		if store.IsUniqueViolation(res.Error) {
			return model.Tag{}, "", httpx.Conflict(httpx.CodeTagAlreadyExists, "target name already exists")
		}
		return model.Tag{}, "", res.Error
	}
	if res.RowsAffected == 0 {
		return model.Tag{}, "", httpx.NotFound("target tag not found")
	}
	return model.Tag{ID: *proposal.TargetTagID, WorkspaceID: proposal.WorkspaceID, Name: name}, outbox.TypeTagRenamed, nil
}

func applyTagDelete(tx *gorm.DB, proposal *model.TagProposal, name string) (model.Tag, string, error) {
	res := tx.Where("id = ? AND workspace_id = ?", *proposal.TargetTagID, proposal.WorkspaceID).Delete(&model.Tag{})
	if res.Error != nil {
		return model.Tag{}, "", res.Error
	}
	if res.RowsAffected == 0 {
		return model.Tag{}, "", httpx.NotFound("target tag not found")
	}
	return model.Tag{ID: *proposal.TargetTagID, WorkspaceID: proposal.WorkspaceID, Name: name}, outbox.TypeTagDeleted, nil
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
	httpx.WriteOK(w, r, http.StatusOK,
		tagDTO{ID: tagRow.ID, WorkspaceID: tagRow.WorkspaceID, Name: tagRow.Name})
}
