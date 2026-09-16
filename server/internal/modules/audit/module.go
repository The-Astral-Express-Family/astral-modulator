// Package audit 模块：只追加审计（architecture §6.10）。
// 业务模块通过 Recorder 接口写审计；本模块只暴露查询 API。
package audit

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

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
	// TODO(phase-6): 时间范围/actor/action 过滤 + cursor 分页；GUI audit timeline 数据源。
	// TODO(phase-2, 服务器级审计的集中登记处): 认证/授权失败写 audit——
	// 现状 Authenticate 中间件只拒不记；需要向 auth.Service 注入 audit recorder
	// 并覆盖 login 失败、refresh 重放、device 兑换失败等路径（security.md 审计要求，
	// TODO.md S4-3（原 §3.2）；auth/middleware.go、auth/service.go、app/router.go
	// 的相关注释均指向本条，不重复登记）。
	r.Get("/workspaces/{workspace_id}/audit", m.list)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	httpx.NotImplemented(w, r, "audit.list", "phase-6", "docs/architecture.md §6.10")
}
