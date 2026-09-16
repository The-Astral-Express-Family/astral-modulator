// Package audit 模块：只追加审计（architecture §6.10）。
// 业务模块通过 Recorder 写审计（recorder.go）；本模块暴露 workspace 级
// 查询 API（round 38 T5 / TODO.md S4-1：GET /workspaces/{workspace_id}/audit）。
// 平台级查询端点 GET /admin/audit 在 admin 模块，复用本包 ListEntries/DTO。
package audit

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// WorkspaceAuthorizer 是 workspace 审计查询的授权前置，语义等同
// auth.RequireWorkspace(r, svc, wsID, audit:read)：非成员 / credential
// 未绑定 → 404 WORKSPACE_NOT_FOUND（不泄露存在性）；成员但 scope 不足 →
// 403 INSUFFICIENT_SCOPE。audit 包不能直接 import auth——auth.Service 写
// 审计时反向依赖本包（recorder.go 的 RecordInTx），直接引用会成环；
// 由装配处注入绑定（cmd/astral-server/main.go、tests fixture）。
type WorkspaceAuthorizer func(r *http.Request, workspaceID string) *httpx.APIError

// Module 是 HTTP 层：路由注册 + 请求/响应编解码。
type Module struct {
	DB *gorm.DB
	// Authorize 是 list 端点的授权前置注入（见 WorkspaceAuthorizer）。
	Authorize WorkspaceAuthorizer
}

// Entry 是审计记录的最小单元（字段对应 00007_audit.sql）。
// 写入用 GormRecorder（recorder.go）：业务事务内走 RecordInTx，
// 事务外走 Record；details 落库前统一 redaction。
type Entry struct {
	WorkspaceID string
	ActorID     string
	Action      string // 稳定动作名：task.claim / credential.create / auth.login ...
	Outcome     string // allowed | denied | error
	TargetType  string
	TargetID    string
	Details     map[string]any // 落库前 redaction
	RequestID   string
}

func (m *Module) RegisterRoutes(r chi.Router) {
	// GET /api/v1/workspaces/{workspace_id}/audit（audit:read）。
	// 只读端点：查询不产生领域事件，无 audit 自记、无 outbox。
	// 服务器级审计（认证/授权失败）已在 S4-3 落地：auth 包各失败分支直接
	// 写 audit（auth/audit.go），不再经集中登记处。
	r.Get("/workspaces/{workspace_id}/audit", m.list)
}

// list 是 GET /workspaces/{workspace_id}/audit：actor_id/action/outcome 过滤
// （均可选）+ 共享 Limit/Cursor；结果以 workspace_id 收口（跨 workspace 与
// 服务器级行对本端点不可见）。排序与游标语义见 ListEntries。
func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	if apiErr := m.Authorize(r, wsID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	outcome, apiErr := ParseOutcome(q.Get("outcome"))
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	rows, next, err := ListEntries(r.Context(), m.DB, ListFilter{
		WorkspaceID: wsID,
		ActorID:     q.Get("actor_id"),
		Action:      q.Get("action"),
		Outcome:     outcome,
		Cursor:      q.Get("cursor"),
		Limit:       httpx.ParseLimit(q.Get("limit"), 50, 200),
	})
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	items := make([]EntryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, ToEntryDTO(row))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}
