// Package document 模块：受管 Markdown 同步（architecture §16、sync-semantics.md；
// round 38 T4 实装，替换 round 37 铺设的 501 桩）。
//
// 七端点：manifest / get / push / delete / conflicts 列表 / 详情 / resolve。
// 核心语义（TODO.md §1.3、§2 S1-2/S1-3/S1-5）：
//   - 乐观并发：push/delete 携带 base_revision(+base_hash)，失配一律落
//     document_conflicts 工件 + 409 DOCUMENT_CONFLICT（P1：不做服务端自动合并，
//     客户端本地合并后经 resolve(merged|manual) 提交）；
//   - 删除是版本化 tombstone（P2）：行保留、revision 续增、可复活；
//   - 每次成功写 = 领域行 + audit + outbox 同事务三件套（硬约束 §1-4）；
//   - 授权按路径前缀切轴（M1）：memory/ → memory:*，其余 → document:*，
//     单点在 scope.go。
//
// 文件拆分：module.go（路由 + manifest/get）、push.go（push/delete 事务核心）、
// conflicts.go（冲突三端点）、store.go（单行查询出口）、dto.go（wire 层）。
package document

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

type Module struct {
	DB   *gorm.DB
	Auth *auth.Service
}

func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces/{workspace_id}/documents/manifest", m.manifest)
	// 文档路径用 chi 尾通配 /*（catch-all）：{path:.*} 是单段正则节点，
	// 匹配不了 "docs/a.md" 这类多段路径（round 38 T4 集成测试发现的脚手架
	// 潜伏问题；契约门把 /* 归一为 openapi 的 {path}）。捕获值经
	// chi.URLParam(r, "*") 读取（不含前导 '/'）。
	// 必须严格 URL 编码；服务端 canonicalize（'/'分隔），拒绝 '..'、绝对
	// 路径、symlink escape、secrets 默认路径（architecture §16）。
	// memory/ 前缀走 memory:read/write scope（M1 裁决，见 TODO.md 决策速查）。
	// manifest 是静态段：chi 按节点类型序（static 先于 catch-all）确定性地
	// 优先命中。
	r.Get("/workspaces/{workspace_id}/documents/*", m.get)
	r.Put("/workspaces/{workspace_id}/documents/*", m.push)
	r.Delete("/workspaces/{workspace_id}/documents/*", m.delete)
	r.Get("/workspaces/{workspace_id}/conflicts", m.listConflicts)
	r.Get("/workspaces/{workspace_id}/conflicts/{conflict_id}", m.getConflict)
	r.Post("/workspaces/{workspace_id}/conflicts/{conflict_id}/resolve", m.resolveConflict)
}

// chiWorkspaceID 是七端点共用的路径参数提取小件。
func chiWorkspaceID(r *http.Request) string {
	return chi.URLParam(r, "workspace_id")
}

// decodePath 提取并校验 /* 尾通配捕获值（chi.URLParam(r, "*")，不含前导 '/'）。
// chi 在 URL 携带转义序列时按 RawPath（转义形式）路由——捕获值保持转义；
// 否则捕获值已是解码值。两种情形统一到「解码后」再交 CanonicalizePath
// （其契约即接收解码值）。失败时已写出 400 envelope，调用方见 false 即返回。
func decodePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := chi.URLParam(r, "*")
	if r.URL.RawPath != "" {
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			httpx.WriteError(w, r, httpx.Invalid("path is not valid URL encoding"))
			return "", false
		}
		raw = decoded
	}
	path, err := CanonicalizePath(raw)
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			httpx.WriteError(w, r, apiErr)
			return "", false
		}
		httpx.WriteError(w, r, httpx.Internal("path validation failed"))
		return "", false
	}
	return path, true
}

// manifestLimitMax/manifestLimitDef：manifest 的 limit 缺省 200、上限 1000
// （openapi 契约即如此，与共享 Limit 参数的 50/200 不同，故本地解析）。
const (
	manifestLimitDef = 200
	manifestLimitMax = 1000
)

// parseManifestLimit 解析 manifest 的 limit 参数（语义对齐 httpx.ParseLimit：
// 缺省/非法/越界回落，不报错），数值按契约自成一档。
func parseManifestLimit(raw string) int {
	if raw == "" {
		return manifestLimitDef
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return manifestLimitDef
	}
	if n > manifestLimitMax {
		return manifestLimitMax
	}
	return n
}

// manifest 是同步基准清单（sync-semantics §10）：path 升序、游标 = 末行 path
// （查询 path > cursor），默认排除 tombstone，include_deleted=true 含之。
func (m *Module) manifest(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	if _, apiErr := requireAnyDocScope(r, m.Auth, wsID, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	limit := parseManifestLimit(q.Get("limit"))
	query := m.DB.WithContext(r.Context()).Model(&model.Document{}).
		Where("workspace_id = ?", wsID)
	if q.Get("include_deleted") != "true" {
		query = query.Where("deleted_at IS NULL")
	}
	if cursor := q.Get("cursor"); cursor != "" {
		query = query.Where("path > ?", cursor)
	}
	var rows []model.Document
	if err := query.Order("path ASC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Path
	}
	items := make([]manifestItemDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toManifestItemDTO(row))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}

// get 按 (workspace_id, path) 精确查；tombstone 也返回（deleted=true、content
// 照常返回）——同步端侦测远端删除的依据（sync-semantics §14）。
func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspace_id")
	path, ok := decodePath(w, r)
	if !ok {
		return
	}
	if _, apiErr := requireDocScope(r, m.Auth, wsID, path, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var row model.Document
	if apiErr := store.First(m.DB.WithContext(r.Context()), &row,
		httpx.NotFound("document not found"), "workspace_id = ? AND path = ?", wsID, path); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toDocumentDTO(row))
}
