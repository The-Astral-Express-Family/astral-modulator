package document

import (
	"net/http"
	"strings"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
)

// memoryPathPrefix 是 documents 内的项目记忆保留前缀（M1 裁决，round 37；
// architecture §6.5/§16）：授权按前缀切换 scope——memory/ → memory:read/write，
// 其余路径 → document:read/write。前缀判断精确区分大小写：Memory/x 是普通
// 文档路径，既不享受也不强制 memory scope；裸文件名 "memory" 不属于该前缀。
const memoryPathPrefix = "memory/"

// isMemoryPath 判断（已 canonicalize 的）path 是否落在保留前缀下。
// 精确匹配 "memory/"：裸文件名 "memory" 不属于该前缀。
func isMemoryPath(path string) bool {
	return strings.HasPrefix(path, memoryPathPrefix)
}

// scopeFor 按 read/write 与前缀选出所需 scope。
func scopeFor(path string, read bool) string {
	if read {
		if isMemoryPath(path) {
			return auth.ScopeMemoryRead
		}
		return auth.ScopeDocumentRead
	}
	if isMemoryPath(path) {
		return auth.ScopeMemoryWrite
	}
	return auth.ScopeDocumentWrite
}

// requireDocScope 是 documents/conflicts 端点按路径选 scope 的授权单点，
// 供七个 handler 统一调用（S1-4）。复用 auth.Service.RequireWorkspaceScopes
// （auth.RequireWorkspace 的底层实现），语义与各模块一致：
// 非成员 / credential 未绑定 → 404 WORKSPACE_NOT_FOUND（不泄露存在性）；
// 成员但 scope 不足 → 403 INSUFFICIENT_SCOPE。
// path 必须已由 CanonicalizePath 归一（本函数不做二次路径校验）；
// read=false 表示写操作（push/delete/resolve）。通过后返回 Principal，
// 供 handler 落 audit 的 actor_id。
func requireDocScope(r *http.Request, svc *auth.Service, workspaceID, path string, read bool) (*auth.Principal, *httpx.APIError) {
	p := auth.PrincipalFrom(r.Context())
	if _, apiErr := svc.RequireWorkspaceScopes(r.Context(), p, workspaceID, scopeFor(path, read)); apiErr != nil {
		return nil, apiErr
	}
	return p, nil
}
