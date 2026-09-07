// Package tag 模块：两步确认的 proposal/confirm（architecture §14）。
// 产品语义：让 Agent “再想一次”，而非自动相似度拒绝。
package tag

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

type Module struct{}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/tags", m.list)
	r.Post("/workspaces/{workspace_id}/tag-proposals", m.propose)
	r.Post("/tag-proposals/{proposal_id}/confirm", m.confirm)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): workspace tag 列表（tag:read）。proposal 第一步响应里的
	//  existing_tags 需要它；CLI `astral tags create` 也会展示。
	httpx.NotImplemented(w, r, "tag.list", "phase-3", "docs/architecture.md §14")
}

func (m *Module) propose(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 第一步 proposal：
	//  - 入参 {action: create|rename|delete, name, target_tag_id?}；
	//  - 规范化（trim/NFC/case-fold）+ 精确唯一约束预检（不做模糊相似度判断）；
	//  - 生成 confirm_code（只存 hash）与 proposal_id（tgp_），TTL ~120s，单次使用；
	//  - 响应带 existing_tags 全量列表，供 Agent 人工比对；
	//  - 支持 Idempotency-Key；写 audit。
	httpx.NotImplemented(w, r, "tag.propose", "phase-3", "docs/architecture.md §14")
}

func (m *Module) confirm(w http.ResponseWriter, r *http.Request) {
	// TODO(phase-3): 第二步确认：
	//  - 校验 proposal 未过期/未使用/同 actor/同 workspace/输入与 canonical 一致；
	//  - 同一事务内再查唯一约束（TOCTOU 兜底）→ 创建/改名/删除 tag；
	//  - 写 audit + outbox；CLI 拼写固定 --confirm（不是文档 typo 的 --comfirm）。
	httpx.NotImplemented(w, r, "tag.confirm", "phase-3", "docs/architecture.md §14")
}
