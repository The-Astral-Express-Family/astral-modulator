// Package memory 模块：组织/项目记忆工作区（architecture §6.5、§15）。
package memory

import "github.com/go-chi/chi/v5"

type Module struct{}

// RegisterRoutes：Memory 的公网 API 形状尚未在文档定稿（TODO.md「待裁决契约」#M1）。
// 候选方向：复用 documents 表 + 保留路径前缀（organization/、projects/<ws>/），
// 只暴露读写与列表，不单独发明第二套文档协议。定稿前不注册任何路由，
// 避免先写死错误契约。
func (m *Module) RegisterRoutes(r chi.Router) {
	_ = r
	// TODO(phase-5): 定稿后注册：
	//   GET  /api/v1/memory/documents           （组织记忆，需 memory:read）
	//   GET/PUT /api/v1/workspaces/{id}/memory/documents/{path}
	// 以及 memory agent 的整理触发/审阅接口（人工 review policy，尤其组织记忆）。
}
