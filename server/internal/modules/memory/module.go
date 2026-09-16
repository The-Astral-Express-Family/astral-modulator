// Package memory 模块：组织/项目记忆工作区（architecture §6.5、§15）。
package memory

import "github.com/go-chi/chi/v5"

type Module struct{}

// RegisterRoutes：M1 已裁决（round 37，TODO.md 决策速查）——Memory 不设独立公网 API：
//   - 项目记忆 = 各 workspace documents 的保留前缀 memory/**，经既有
//     /workspaces/{id}/documents/* 端点读写，按前缀改用 memory:read/write scope；
//   - 组织记忆 = 保留 workspace `org-memory`（bootstrap 注册惰性种子 + owner
//     membership），同样走 documents 端点；
//   - memory agent 的编排属客户端行为（architecture §15 工作流），服务端只提供
//     文档读写 + audit + 事件，MVP 不建整理触发/审阅接口。
//
// 因此本模块永远不注册路由；保留包位作为该裁决的活文档。
func (m *Module) RegisterRoutes(r chi.Router) {
	_ = r
}
