// versions.go 是历史版本读端点（00017；TODO §1.2 P3 解冻）：listVersions /
// getVersion。文档路径本体是 chi 尾通配，子资源无法挂在
// /documents/{path}/versions 下，故独立成 /document-versions 资源（与
// /conflicts 同层），path 经必填 query 参数传入。授权走 requireDocScope 的
// 路径轴（memory/ 前缀 → memory:read），与 get 同口径。
//
// 恢复不设写端点：取回旧内容后走 push + pinned base（--base-revision /
// --base-hash），复用既有乐观并发——恢复操作本身也要过 CAS，不会覆盖别人。
package document

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// versionsLimitDef/versionsLimitMax：版本列表 limit 缺省 50、上限 200
// （openapi 共享 Limit 参数的 50/200 档，与 manifest 的 200/1000 自成一档不同）。
const (
	versionsLimitDef = 50
	versionsLimitMax = 200
)

// versionItemDTO 对应 openapi DocumentVersion schema：列表形状，不含 content
// （单版可达 1MiB，全文走详情端点，与 conflicts 列表/详情同惯例）。
type versionItemDTO struct {
	Path        string `json:"path"`
	Revision    int64  `json:"revision"`
	ContentHash string `json:"content_hash"`
	Size        int    `json:"size"`
	Kind        string `json:"kind"`
	Deleted     bool   `json:"deleted"`
	ActorID     string `json:"actor_id"`
	CreatedAt   string `json:"created_at"`
}

func toVersionItemDTO(row model.DocumentVersion) versionItemDTO {
	return versionItemDTO{
		Path:        row.Path,
		Revision:    row.Revision,
		ContentHash: row.ContentHash,
		Size:        len(row.Content),
		Kind:        row.Kind,
		Deleted:     row.Deleted,
		ActorID:     row.ActorID,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// versionDetailDTO 对应 openapi DocumentVersionDetail：列表形状 + 完整内容。
type versionDetailDTO struct {
	versionItemDTO
	Content string `json:"content"`
}

// listVersions：revision 降序、游标 = 末行 revision（查询 revision < cursor）。
// 历史只含被取代版本——当前版本仍在 documents 行，走 get。文档不存在 → 404
// （与 get 同语义）；存在但从未被取代 → 空 items。
func (m *Module) listVersions(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	path, ok := decodeVersionsPath(w, r)
	if !ok {
		return
	}
	if _, apiErr := requireDocScope(r, m.Auth, wsID, path, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	// 存在性单独校验：区分「文档不存在」（404）与「尚无历史」（空列表）。
	var doc model.Document
	if apiErr := store.First(m.DB.WithContext(r.Context()), &doc,
		httpx.NotFound("document not found"), "workspace_id = ? AND path = ?", wsID, path); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	limit := httpx.ParseLimit(q.Get("limit"), versionsLimitDef, versionsLimitMax)
	query := m.DB.WithContext(r.Context()).Model(&model.DocumentVersion{}).
		Where("workspace_id = ? AND path = ?", wsID, path)
	if cursor := q.Get("cursor"); cursor != "" {
		rev, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || rev < 1 {
			httpx.WriteError(w, r, invalidField("cursor", "cursor must be a positive revision integer"))
			return
		}
		query = query.Where("revision < ?", rev)
	}
	var rows []model.DocumentVersion
	if err := query.Order("revision DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = strconv.FormatInt(rows[len(rows)-1].Revision, 10)
	}
	items := make([]versionItemDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toVersionItemDTO(row))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}

// getVersion：单版本取回（含完整内容）。revision 是路径参数（≥1）；
// path 是必填 query。当前版本不在此端点的命中范围（见 listVersions）。
func (m *Module) getVersion(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	path, ok := decodeVersionsPath(w, r)
	if !ok {
		return
	}
	if _, apiErr := requireDocScope(r, m.Auth, wsID, path, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	rev, err := strconv.ParseInt(chi.URLParam(r, "revision"), 10, 64)
	if err != nil || rev < 1 {
		httpx.WriteError(w, r, invalidField("revision", "revision must be a positive integer"))
		return
	}
	var row model.DocumentVersion
	if apiErr := store.First(m.DB.WithContext(r.Context()), &row,
		httpx.NotFound("document version not found"),
		"workspace_id = ? AND path = ? AND revision = ?", wsID, path, rev); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	detail := versionDetailDTO{versionItemDTO: toVersionItemDTO(row), Content: row.Content}
	httpx.WriteOK(w, r, http.StatusOK, detail)
}

// decodeVersionsPath 从 query 参数 path 提取并 canonicalize（尾通配不可用，
// 见文件头注释）。失败时已写出 400 envelope，调用方见 false 即返回。
func decodeVersionsPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		httpx.WriteError(w, r, invalidField("path", "path query parameter is required"))
		return "", false
	}
	path, err := CanonicalizePath(raw)
	if err != nil {
		// CanonicalizePath 的全部失败返回都是 *httpx.APIError（decodePath 同款）。
		httpx.WriteError(w, r, err.(*httpx.APIError))
		return "", false
	}
	return path, true
}
