// Package auth 是认证/授权模块（architecture §6.1）。
// 本文件固定 scope 词表；服务端授权只认最终 scope 集合，不信任客户端传的 role 字符串。
package auth

// Scope 词表，与 docs/architecture.md §10 一一对应。
// 变更属于协议变更：同步 openapi.yaml 的 security 描述与 TODO.md 登记。
const (
	ScopeWorkspaceRead         = "workspace:read"
	ScopeWorkspaceWrite        = "workspace:write"
	ScopeWorkspaceManageMember = "workspace:manage_members"

	ScopeTaskRead     = "task:read"
	ScopeTaskWrite    = "task:write"
	ScopeTaskClaim    = "task:claim"
	ScopeTaskOverride = "task:override"

	ScopeTagRead  = "tag:read"
	ScopeTagWrite = "tag:write"

	ScopeDocumentRead  = "document:read"
	ScopeDocumentWrite = "document:write"

	ScopeMemoryRead  = "memory:read"
	ScopeMemoryWrite = "memory:write"

	ScopeMessageRead = "message:read"
	ScopeMessageSend = "message:send"

	ScopePresenceWrite = "presence:write"

	ScopeAuditRead = "audit:read"

	ScopeAgentManage       = "agent:manage"
	ScopeIntegrationUse    = "integration:use"
	ScopeIntegrationManage = "integration:manage"

	// 平台全局 scope（round 33）：与 workspace scope 分轴，只随平台角色
	// 授予，见 GlobalScopesFor。
	ScopePlatformUsersRead   = "platform:users:read"
	ScopePlatformUsersManage = "platform:users:manage"
	ScopePlatformCredsManage = "platform:credentials:manage"
	// round 37：平台级审计查询（GET /admin/audit，服务器级记录入口）。
	ScopePlatformAuditRead = "platform:audit:read"
)

// AllScopes 调试用全集；不得用于默认授权。
var AllScopes = []string{
	ScopeWorkspaceRead, ScopeWorkspaceWrite, ScopeWorkspaceManageMember,
	ScopeTaskRead, ScopeTaskWrite, ScopeTaskClaim, ScopeTaskOverride,
	ScopeTagRead, ScopeTagWrite,
	ScopeDocumentRead, ScopeDocumentWrite,
	ScopeMemoryRead, ScopeMemoryWrite,
	ScopeMessageRead, ScopeMessageSend,
	ScopePresenceWrite,
	ScopeAuditRead,
	ScopeAgentManage, ScopeIntegrationUse, ScopeIntegrationManage,
}

// GlobalScopesFor 是平台角色 → 全局 scope bundle（与 RoleToScopes 同哲学：
// 授权只认最终 scope 集合）。human ∈ {admin,user}；agent/service 固化 kind，
// 永无平台特权——agent credential 的能力仍只由 credential scopes 决定。
var GlobalScopesFor = map[string][]string{
	"admin":   {ScopePlatformUsersRead, ScopePlatformUsersManage, ScopePlatformCredsManage, ScopePlatformAuditRead},
	"user":    {},
	"agent":   {},
	"service": {},
}

// Role 是 scope bundle。角色定义变更需评估已有 credential 的语义。
var RoleToScopes = map[string][]string{
	"viewer":      {ScopeWorkspaceRead, ScopeTaskRead, ScopeTagRead, ScopeDocumentRead, ScopeMemoryRead, ScopeMessageRead},
	"contributor": {ScopeWorkspaceRead, ScopeTaskRead, ScopeTaskWrite, ScopeTaskClaim, ScopeTagRead, ScopeTagWrite, ScopeDocumentRead, ScopeDocumentWrite, ScopeMemoryRead, ScopeMemoryWrite, ScopeMessageRead, ScopeMessageSend, ScopePresenceWrite},
	// agent 与 contributor 的 bundle 刻意一致（D9：agent 是 contributor 能力
	// 的凭证化载体，实际权限再经 credential scopes 收窄）；分化时在此拆，勿默认同步。
	"agent":      {ScopeWorkspaceRead, ScopeTaskRead, ScopeTaskWrite, ScopeTaskClaim, ScopeTagRead, ScopeTagWrite, ScopeDocumentRead, ScopeDocumentWrite, ScopeMemoryRead, ScopeMemoryWrite, ScopeMessageRead, ScopeMessageSend, ScopePresenceWrite},
	"maintainer": {ScopeWorkspaceRead, ScopeWorkspaceWrite, ScopeTaskRead, ScopeTaskWrite, ScopeTaskClaim, ScopeTaskOverride, ScopeTagRead, ScopeTagWrite, ScopeDocumentRead, ScopeDocumentWrite, ScopeMemoryRead, ScopeMemoryWrite, ScopeMessageRead, ScopeMessageSend, ScopePresenceWrite, ScopeAuditRead, ScopeAgentManage},
	"owner":      AllScopes, // 含 workspace:manage_members 等
}
