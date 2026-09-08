// Package workspace 模块（architecture §6.2、§11）：
// workspace CRUD、成员管理、agent identity 与 credential 生命周期。
package workspace

import (
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

type Module struct {
	DB    *gorm.DB
	Audit *audit.GormRecorder
	Hub   *event.Hub
	// Auth 提供 scope 解析与 credential 签发/吊销。
	Auth *auth.Service
}

// RegisterRoutes 全部端点已实装（原 501 桩移除）。
// 路径一律扁平注册（chi 不允许跨模块重复 Route 子挂载）。
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces", m.create)
	r.Get("/workspaces", m.list)
	r.Get("/workspaces/{workspace_id}", m.get)
	r.Patch("/workspaces/{workspace_id}", m.update)
	r.Get("/workspaces/{workspace_id}/members", m.listMembers)
	r.Post("/workspaces/{workspace_id}/members", m.addMember)
	r.Patch("/workspaces/{workspace_id}/members/{actor_id}", m.updateMember)
	r.Delete("/workspaces/{workspace_id}/members/{actor_id}", m.removeMember)
	r.Get("/workspaces/{workspace_id}/agents", m.listAgents)
	r.Post("/workspaces/{workspace_id}/agents", m.createAgent)
	r.Post("/agents/{agent_id}/credentials", m.createCredential)
	r.Delete("/agents/{agent_id}/credentials/{credential_id}", m.revokeCredential)
}

// ---- DTO ----

type workspaceDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toWorkspaceDTO(ws model.Workspace) workspaceDTO {
	return workspaceDTO{
		ID: ws.ID, Name: ws.Name, Slug: ws.Slug,
		CreatedAt: ws.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: ws.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type memberDTO struct {
	Actor actorDTO `json:"actor"`
	Role  string   `json:"role"`
}

type actorDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

// requireWorkspace 加载 workspace 并走标准授权前置（语义见
// auth.RequireWorkspaceScopes：非成员 404 / scope 不足 403）。
func (m *Module) requireWorkspace(r *http.Request, workspaceID string, need ...string) (*model.Workspace, map[string]bool, *httpx.APIError) {
	p := auth.PrincipalFrom(r.Context())
	var ws model.Workspace
	err := m.DB.WithContext(r.Context()).First(&ws, "id = ?", workspaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, &httpx.APIError{Status: 404, Code: httpx.CodeWorkspaceNotFound, Message: "workspace not found"}
	}
	if err != nil {
		return nil, nil, &httpx.APIError{Status: 500, Code: httpx.CodeInternalError, Message: "workspace lookup failed"}
	}
	scopes, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, workspaceID, need...)
	if apiErr != nil {
		return nil, nil, apiErr
	}
	return &ws, scopes, nil
}

// ---- workspace CRUD ----

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "name required (1-200 chars)"})
		return
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = slugify(in.Name)
	}
	if slug == "" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "slug could not be derived; provide slug"})
		return
	}

	ws := model.Workspace{ID: ids.New(ids.Workspace), Name: in.Name, Slug: slug, CreatedBy: p.ActorID}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ws).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.WorkspaceMember{WorkspaceID: ws.ID, ActorID: p.ActorID, Role: "owner"}).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.create", Outcome: "allowed",
			TargetType: "workspace", TargetID: ws.ID,
		})
	})
	if err != nil {
		writeDBError(w, r, err, "workspace name/slug already taken", httpx.CodeWorkspaceNameTaken)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, toWorkspaceDTO(ws))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())

	// 可见性 = 成员关系。astral init 依赖 ?name=<exact-or-slug> 精确解析
	// （architecture §11）：空列表表示“不存在或不可见”，CLI 据此提示可创建。
	var memberships []model.WorkspaceMember
	if err := m.DB.WithContext(r.Context()).Where("actor_id = ?", p.ActorID).Find(&memberships).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	wsIDs := make([]string, 0, len(memberships))
	for _, mem := range memberships {
		wsIDs = append(wsIDs, mem.WorkspaceID)
	}

	query := m.DB.WithContext(r.Context()).Model(&model.Workspace{})
	if len(wsIDs) > 0 {
		query = query.Where("id IN ?", wsIDs)
	} else {
		query = query.Where("1 = 0") // 无成员关系：恒空
	}
	if name := r.URL.Query().Get("name"); name != "" {
		query = query.Where("name = ? OR slug = ?", name, name)
	}

	var rows []model.Workspace
	if err := query.Order("created_at ASC").Limit(200).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]workspaceDTO, 0, len(rows))
	for _, ws := range rows {
		items = append(items, toWorkspaceDTO(ws))
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"))
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toWorkspaceDTO(*ws))
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceWrite)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		Name *string `json:"name"`
		Slug *string `json:"slug"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	updates := map[string]any{"updated_at": time.Now()}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "name cannot be empty"})
			return
		}
		updates["name"] = n
	}
	if in.Slug != nil {
		s := slugify(*in.Slug)
		if s == "" {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid slug"})
			return
		}
		updates["slug"] = s
	}
	if err := m.DB.WithContext(r.Context()).Model(&model.Workspace{}).Where("id = ?", ws.ID).Updates(updates).Error; err != nil {
		writeDBError(w, r, err, "workspace name/slug already taken", httpx.CodeWorkspaceNameTaken)
		return
	}
	var fresh model.Workspace
	if err := m.DB.WithContext(r.Context()).First(&fresh, "id = ?", ws.ID).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toWorkspaceDTO(fresh))
}

// ---- membership ----

func (m *Module) listMembers(w http.ResponseWriter, r *http.Request) {
	_, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var members []model.WorkspaceMember
	if err := m.DB.WithContext(r.Context()).Where("workspace_id = ?", chi.URLParam(r, "workspace_id")).Find(&members).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]memberDTO, 0, len(members))
	for _, mem := range members {
		var actor model.Actor
		if err := m.DB.WithContext(r.Context()).First(&actor, "id = ?", mem.ActorID).Error; err != nil {
			continue
		}
		items = append(items, memberDTO{
			Actor: actorDTO{ID: actor.ID, Kind: actor.Kind, DisplayName: actor.DisplayName},
			Role:  mem.Role,
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

func (m *Module) addMember(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		ActorID string `json:"actor_id"`
		Role    string `json:"role"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if !validRole(in.Role) {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid role"})
		return
	}
	if in.Role == "owner" {
		// architecture §22：owner 提升高风险动作走 approval 状态机（未实装，先拒绝）。
		httpx.WriteError(w, r, &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "owner assignment requires approval flow (not yet implemented)"})
		return
	}
	var actor model.Actor
	if err := m.DB.WithContext(r.Context()).First(&actor, "id = ?", in.ActorID).Error; err != nil {
		httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "actor not found"})
		return
	}
	mem := model.WorkspaceMember{WorkspaceID: ws.ID, ActorID: in.ActorID, Role: in.Role}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&mem).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.member.add", Outcome: "allowed",
			TargetType: "actor", TargetID: in.ActorID,
			Details: map[string]any{"role": in.Role},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
			"actor_id": in.ActorID, "role": in.Role, "change": "added",
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, memberDTO{
		Actor: actorDTO{ID: actor.ID, Kind: actor.Kind, DisplayName: actor.DisplayName}, Role: in.Role,
	})
}

func (m *Module) updateMember(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	actorID := chi.URLParam(r, "actor_id")
	var in struct {
		Role string `json:"role"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if !validRole(in.Role) || in.Role == "owner" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid role (owner requires approval flow)"})
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.WorkspaceMember{}).
			Where("workspace_id = ? AND actor_id = ?", ws.ID, actorID).
			Update("role", in.Role)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "member not found"}
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.member.update", Outcome: "allowed",
			TargetType: "actor", TargetID: actorID, Details: map[string]any{"role": in.Role},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
			"actor_id": actorID, "role": in.Role, "change": "updated",
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) removeMember(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	actorID := chi.URLParam(r, "actor_id")
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("workspace_id = ? AND actor_id = ?", ws.ID, actorID).Delete(&model.WorkspaceMember{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "member not found"}
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.member.remove", Outcome: "allowed",
			TargetType: "actor", TargetID: actorID,
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
			"actor_id": actorID, "change": "removed",
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- agent identity / credential ----

func (m *Module) listAgents(w http.ResponseWriter, r *http.Request) {
	_, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeAgentManage)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// TODO(phase-3): 当前无 agent-workspace 绑定模型，返回服务器全局 agent 列表
	// （仅 id/kind/display_name，低敏感）。绑定模型定稿后改为按 workspace 过滤
	// （登记 TODO.md T-ws-7）。
	var actors []model.Actor
	if err := m.DB.WithContext(r.Context()).
		Where("kind IN ('agent','service')").Find(&actors).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]actorDTO, 0, len(actors))
	for _, a := range actors {
		items = append(items, actorDTO{ID: a.ID, Kind: a.Kind, DisplayName: a.DisplayName})
	}
	httpx.WriteOK(w, r, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

func (m *Module) createAgent(w http.ResponseWriter, r *http.Request) {
	ws, _, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeAgentManage)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		DisplayName string `json:"display_name"`
		Kind        string `json:"kind"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Kind == "" {
		in.Kind = "agent"
	}
	if in.Kind != "agent" && in.Kind != "service" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "kind must be agent or service"})
		return
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "display_name required"})
		return
	}
	prefix := ids.Agent
	if in.Kind == "service" {
		prefix = ids.Service
	}
	actor := model.Actor{ID: ids.New(prefix), Kind: in.Kind, DisplayName: in.DisplayName}
	if err := m.DB.WithContext(r.Context()).Create(&actor).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	_ = m.Audit.Record(r.Context(), audit.Entry{
		WorkspaceID: ws.ID, ActorID: p.ActorID,
		Action: "agent.create", Outcome: "allowed",
		TargetType: "actor", TargetID: actor.ID,
	})
	httpx.WriteOK(w, r, http.StatusCreated, actorDTO{ID: actor.ID, Kind: actor.Kind, DisplayName: actor.DisplayName})
}

func (m *Module) createCredential(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	agentID := chi.URLParam(r, "agent_id")
	var actor model.Actor
	if err := m.DB.WithContext(r.Context()).First(&actor, "id = ?", agentID).Error; err != nil {
		httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "agent not found"})
		return
	}
	if actor.Kind != "agent" && actor.Kind != "service" {
		httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "not an agent/service actor"})
		return
	}
	var in struct {
		Scopes    []string `json:"scopes"`
		Workspace string   `json:"workspace_id"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	// agent:manage 授权：绑定 workspace 的 credential 校验该 workspace 权限；
	// 全局 credential 仅 human 可发（MVP 简化，TODO(phase-2): 服务器级 scope）。
	if in.Workspace != "" {
		if _, _, apiErr := m.requireWorkspace(r, in.Workspace, auth.ScopeAgentManage); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
			return
		}
	} else if apiErr := requireHuman(r); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var exp *time.Time
	if in.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, r, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "invalid expires_at (RFC3339)"})
			return
		}
		exp = &t
	}
	var wsRef *string
	if in.Workspace != "" {
		ws := in.Workspace
		wsRef = &ws
	}
	issued, err := m.Auth.IssueCredential(r.Context(), auth.CreateCredentialInput{
		ActorID: agentID, Kind: actor.Kind, Scopes: in.Scopes,
		Workspace: wsRef, CreatedBy: p.ActorID, ExpiresAt: exp,
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	err = m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: in.Workspace, ActorID: p.ActorID,
			Action: "credential.create", Outcome: "allowed",
			TargetType: "credential", TargetID: issued.CredentialID,
			Details: map[string]any{"scopes": in.Scopes},
		}); err != nil {
			return err
		}
		if in.Workspace != "" {
			return event.EmitTx(tx, event.TypeSecurityCredentialCreated, in.Workspace, p.ActorID, 0, map[string]any{
				"credential_id": issued.CredentialID, "actor_id": agentID,
			})
		}
		return nil
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, issued)
}

func (m *Module) revokeCredential(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	credentialID := chi.URLParam(r, "credential_id")
	var cred model.Credential
	if err := m.DB.WithContext(r.Context()).First(&cred, "id = ?", credentialID).Error; err != nil {
		httpx.WriteError(w, r, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "credential not found"})
		return
	}
	wsScope := ""
	if cred.WorkspaceID != nil {
		wsScope = *cred.WorkspaceID
	}
	if wsScope != "" {
		if _, _, apiErr := m.requireWorkspace(r, wsScope, auth.ScopeAgentManage); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
			return
		}
	} else if apiErr := requireHuman(r); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if err := m.Auth.RevokeCredential(r.Context(), credentialID); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: wsScope, ActorID: p.ActorID,
			Action: "credential.revoke", Outcome: "allowed",
			TargetType: "credential", TargetID: credentialID,
		}); err != nil {
			return err
		}
		if wsScope != "" {
			return event.EmitTx(tx, event.TypeSecurityCredentialRevoked, wsScope, p.ActorID, 0, map[string]any{
				"credential_id": credentialID, "actor_id": cred.ActorID,
			})
		}
		return nil
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func requireHuman(r *http.Request) *httpx.APIError {
	p := auth.PrincipalFrom(r.Context())
	if p == nil || !p.IsHuman() {
		return &httpx.APIError{Status: 403, Code: httpx.CodeInsufficientScope, Message: "human session required for unbound credentials"}
	}
	return nil
}

func validRole(role string) bool {
	switch role {
	case "viewer", "contributor", "agent", "maintainer", "owner":
		return true
	}
	return false
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeDBError(w http.ResponseWriter, r *http.Request, err error, conflictMsg, conflictCode string) {
	// 唯一约束冲突翻译（可移植判断：PG 23505 文案 / sqlite UNIQUE 文案）。
	msg := err.Error()
	if strings.Contains(msg, "duplicate key") || strings.Contains(msg, "UNIQUE constraint") || strings.Contains(msg, "constraint failed") {
		httpx.WriteError(w, r, &httpx.APIError{Status: http.StatusConflict, Code: conflictCode, Message: conflictMsg})
		return
	}
	httpx.RespondError(w, r, err)
}
