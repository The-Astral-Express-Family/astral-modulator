// Package workspace 模块（architecture §6.2、§11）：
// workspace CRUD、成员管理、agent identity 与 credential 生命周期。
package workspace

import (
	"net/http"
	"sort"
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

type Module struct {
	DB *gorm.DB
	// Auth 提供 scope 解析与 credential 签发/吊销。
	Auth *auth.Service
	// WebBaseURL/PublicURL 供邀请注册链接拼装（回退链同 device flow，
	// 见 auth.ResolveWebBaseURL）；可留空（兜底请求 Host）。
	WebBaseURL string
	PublicURL  string
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
	r.Post("/workspaces/{workspace_id}/invitations", m.createInvitation)
	r.Get("/workspaces/{workspace_id}/invitations", m.listInvitations)
	r.Post("/invitations/{invitation_id}/revoke", m.revokeInvitation)
	m.registerApprovalRoutes(r)
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
	Actor auth.ActorDTO `json:"actor"`
	Role  string        `json:"role"`
}

// requireWorkspace 加载 workspace 并走标准授权前置（语义见
// auth.RequireWorkspaceScopes：非成员 404 / scope 不足 403）。
// 需要具体 scope 集合的调用方直接调 m.Auth.RequireWorkspaceScopes。
func (m *Module) requireWorkspace(r *http.Request, workspaceID string, need ...string) (*model.Workspace, *httpx.APIError) {
	p := auth.PrincipalFrom(r.Context())
	var ws model.Workspace
	if apiErr := store.First(m.DB.WithContext(r.Context()), &ws,
		&httpx.APIError{Status: 404, Code: httpx.CodeWorkspaceNotFound, Message: "workspace not found"},
		"id = ?", workspaceID); apiErr != nil {
		return nil, apiErr
	}
	if _, apiErr := m.Auth.RequireWorkspaceScopes(r.Context(), p, workspaceID, need...); apiErr != nil {
		return nil, apiErr
	}
	return &ws, nil
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
		httpx.WriteError(w, r, httpx.Invalid("name required (1-200 chars)"))
		return
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = slugify(in.Name)
	}
	if slug == "" {
		httpx.WriteError(w, r, httpx.Invalid("slug could not be derived; provide slug"))
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
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"))
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toWorkspaceDTO(*ws))
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceWrite)
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
	updates := map[string]any{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			httpx.WriteError(w, r, httpx.Invalid("name cannot be empty"))
			return
		}
		updates["name"] = n
	}
	if in.Slug != nil {
		s := slugify(*in.Slug)
		if s == "" {
			httpx.WriteError(w, r, httpx.Invalid("invalid slug"))
			return
		}
		updates["slug"] = s
	}
	p := auth.PrincipalFrom(r.Context())
	// 变更与审计同事务（architecture §6.10）；workspace 改名不发领域事件
	// （无对应事件类型，绑定 id 不受改名影响，客户端无需事件通知）。
	var fresh model.Workspace
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Workspace{}).Where("id = ?", ws.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&fresh, "id = ?", ws.ID).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.update", Outcome: "allowed",
			TargetType: "workspace", TargetID: ws.ID,
			Details: map[string]any{"fields": updatedFields(updates)},
		})
	})
	if err != nil {
		writeDBError(w, r, err, "workspace name/slug already taken", httpx.CodeWorkspaceNameTaken)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toWorkspaceDTO(fresh))
}

// ---- membership ----

func (m *Module) listMembers(w http.ResponseWriter, r *http.Request) {
	_, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceRead)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var members []model.WorkspaceMember
	if err := m.DB.WithContext(r.Context()).Where("workspace_id = ?", chi.URLParam(r, "workspace_id")).Find(&members).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// actor 展示信息一次 IN 查询取回（model.ActorsByIDs）；
	// DB 故障如实上抛，不呈现为「短列表」。
	actorIDs := make([]string, 0, len(members))
	for _, mem := range members {
		actorIDs = append(actorIDs, mem.ActorID)
	}
	actorsByID, err := model.ActorsByIDs(r.Context(), m.DB, actorIDs)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]memberDTO, 0, len(members))
	for _, mem := range members {
		actor, ok := actorsByID[mem.ActorID]
		if !ok {
			continue // 成员行在而 actor 行缺失（不应发生）：跳过而非 500
		}
		items = append(items, memberDTO{
			Actor: auth.ToActorDTO(actor),
			Role:  mem.Role,
		})
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

func (m *Module) addMember(w http.ResponseWriter, r *http.Request) {
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
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
		httpx.WriteError(w, r, httpx.Invalid("invalid role"))
		return
	}
	if in.Role == "owner" {
		// architecture §22：owner 提升走 approval 状态机（workspace/approval.go）。
		httpx.WriteError(w, r, httpx.Invalid("owner assignment requires approval: POST /workspaces/{id}/approvals"))
		return
	}
	var actor model.Actor
	if apiErr := store.First(m.DB.WithContext(r.Context()), &actor,
		httpx.NotFound("actor not found"), "id = ?", in.ActorID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
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
		return outbox.EmitTx(tx, outbox.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
			"actor_id": in.ActorID, "role": in.Role, "change": "added",
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, memberDTO{
		Actor: auth.ToActorDTO(actor), Role: in.Role,
	})
}

func (m *Module) updateMember(w http.ResponseWriter, r *http.Request) {
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
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
	if !validRole(in.Role) {
		httpx.WriteError(w, r, httpx.Invalid("invalid role"))
		return
	}
	if in.Role == "owner" {
		// architecture §22：owner 变更走 approval 状态机，不经本端点。
		httpx.WriteError(w, r, httpx.Invalid("owner change requires approval: POST /workspaces/{id}/approvals"))
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
			return httpx.NotFound("member not found")
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.member.update", Outcome: "allowed",
			TargetType: "actor", TargetID: actorID, Details: map[string]any{"role": in.Role},
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
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
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeWorkspaceManageMember)
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
			return httpx.NotFound("member not found")
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "workspace.member.remove", Outcome: "allowed",
			TargetType: "actor", TargetID: actorID,
		}); err != nil {
			return err
		}
		return outbox.EmitTx(tx, outbox.TypeWorkspaceMemberChanged, ws.ID, p.ActorID, 0, map[string]any{
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
	wsID := chi.URLParam(r, "workspace_id")
	if _, apiErr := m.requireWorkspace(r, wsID, auth.ScopeAgentManage); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// workspace 内的 agent 视图（D9/T-ws-7）：membership 行（role='agent'）∪
	// 有效 credential 绑定本 workspace 的 actor——后者覆盖存量只发过 credential、
	// 无 membership 行的 agent。「谁在 workspace」的事实来源只有 membership，
	// credential 绑定是纯授权载体（scope 容器），故两路并集去重。
	var members []model.WorkspaceMember
	if err := m.DB.WithContext(r.Context()).
		Where("workspace_id = ? AND role = 'agent'", wsID).Find(&members).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	var creds []model.Credential
	if err := m.DB.WithContext(r.Context()).
		Where("workspace_id = ? AND revoked_at IS NULL", wsID).Find(&creds).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	// IN 查询对重复 id 天然去重（行按主键唯一），无需先收集时去重。
	actorIDs := make([]string, 0, len(members)+len(creds))
	for _, mem := range members {
		actorIDs = append(actorIDs, mem.ActorID)
	}
	for _, c := range creds {
		actorIDs = append(actorIDs, c.ActorID)
	}
	items := make([]auth.ActorDTO, 0, len(actorIDs))
	if len(actorIDs) > 0 {
		var actors []model.Actor
		if err := m.DB.WithContext(r.Context()).
			Where("id IN ? AND kind IN ('agent','service')", actorIDs).
			Order("created_at ASC").Find(&actors).Error; err != nil {
			httpx.RespondError(w, r, err)
			return
		}
		for _, a := range actors {
			items = append(items, auth.ToActorDTO(a))
		}
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, ""))
}

func (m *Module) createAgent(w http.ResponseWriter, r *http.Request) {
	ws, apiErr := m.requireWorkspace(r, chi.URLParam(r, "workspace_id"), auth.ScopeAgentManage)
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
		httpx.WriteError(w, r, httpx.Invalid("kind must be agent or service"))
		return
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		httpx.WriteError(w, r, httpx.Invalid("display_name required"))
		return
	}
	prefix := ids.Agent
	if in.Kind == "service" {
		prefix = ids.Service
	}
	actor := model.Actor{ID: ids.New(prefix), Kind: in.Kind, DisplayName: in.DisplayName}
	// agent actor 创建即落 workspace（D9：membership 行是归属的唯一事实，
	// 人/agent 通用；credential 绑定只是 scope 载体）。不发领域事件
	// （无对应事件类型，待契约补充）；审计随事务落库。
	err := m.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&actor).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.WorkspaceMember{
			WorkspaceID: ws.ID, ActorID: actor.ID, Role: "agent",
		}).Error; err != nil {
			return err
		}
		return audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: ws.ID, ActorID: p.ActorID,
			Action: "agent.create", Outcome: "allowed",
			TargetType: "actor", TargetID: actor.ID,
		})
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusCreated, auth.ToActorDTO(actor))
}

func (m *Module) createCredential(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	agentID := chi.URLParam(r, "agent_id")
	var actor model.Actor
	if apiErr := store.First(m.DB.WithContext(r.Context()), &actor,
		httpx.NotFound("agent not found"), "id = ?", agentID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if actor.Kind != "agent" && actor.Kind != "service" {
		httpx.WriteError(w, r, httpx.Invalid("not an agent/service actor"))
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
		if _, apiErr := m.requireWorkspace(r, in.Workspace, auth.ScopeAgentManage); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
			return
		}
	} else if apiErr := auth.RequireHuman(r, "human session required for unbound credentials"); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var exp *time.Time
	if in.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, r, httpx.Invalid("invalid expires_at (RFC3339)"))
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
			return outbox.EmitTx(tx, outbox.TypeSecurityCredentialCreated, in.Workspace, p.ActorID, 0, map[string]any{
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
	if apiErr := store.First(m.DB.WithContext(r.Context()), &cred,
		httpx.NotFound("credential not found"), "id = ?", credentialID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	wsScope := ""
	if cred.WorkspaceID != nil {
		wsScope = *cred.WorkspaceID
	}
	if wsScope != "" {
		if _, apiErr := m.requireWorkspace(r, wsScope, auth.ScopeAgentManage); apiErr != nil {
			httpx.WriteError(w, r, apiErr)
			return
		}
	} else if apiErr := auth.RequireHuman(r, "human session required for unbound credentials"); apiErr != nil {
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
			return outbox.EmitTx(tx, outbox.TypeSecurityCredentialRevoked, wsScope, p.ActorID, 0, map[string]any{
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

// updatedFields 返回 updates map 的键名列表（审计用，不落变更值）。
func updatedFields(updates map[string]any) []string {
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeDBError(w http.ResponseWriter, r *http.Request, err error, conflictMsg, conflictCode string) {
	// 唯一约束冲突翻译（可移植判断单点在 store.IsUniqueViolation）。
	if store.IsUniqueViolation(err) {
		httpx.WriteError(w, r, httpx.Conflict(conflictCode, conflictMsg))
		return
	}
	httpx.RespondError(w, r, err)
}
