// API 类型定义 —— 手工对齐 api/openapi.yaml。
// TODO(phase-2): 换成 openapi-typescript 从 openapi.yaml 生成，消除手工同步漂移。
// 在此之前：改契约必须同时改 openapi.yaml 和本文件，并在 TODO.md 登记。

export type ID = string

// 与 api/schemas/error.json 的 ErrorCode enum 保持一致（含 NOT_FOUND 等
// 服务端 httpx/errors.go 全量稳定码）。
export type ErrorCode =
  | 'AUTH_REQUIRED'
  | 'TOKEN_EXPIRED'
  | 'TOKEN_REVOKED'
  | 'INSUFFICIENT_SCOPE'
  | 'SERVER_NOT_FOUND'
  | 'WORKSPACE_NOT_FOUND'
  | 'WORKSPACE_ALREADY_BOUND'
  | 'WORKSPACE_NAME_TAKEN'
  | 'TASK_NOT_FOUND'
  | 'TASK_ALREADY_CLAIMED'
  | 'TASK_LEASE_EXPIRED'
  | 'TAG_PROPOSAL_EXPIRED'
  | 'TAG_ALREADY_EXISTS'
  | 'APPROVAL_EXPIRED'
  | 'INVITE_INVALID'
  | 'EMAIL_TAKEN'
  | 'REVISION_CONFLICT'
  | 'DOCUMENT_CONFLICT'
  | 'RATE_LIMITED'
  | 'CLIENT_VERSION_UNSUPPORTED'
  | 'VALIDATION_FAILED'
  | 'NOT_FOUND'
  | 'AUTHORIZATION_PENDING'
  | 'SLOW_DOWN'
  | 'INTERNAL_ERROR'
  | 'NOT_IMPLEMENTED'

export interface ApiErrorBody {
  code: ErrorCode
  message: string
  retryable: boolean
  details?: Record<string, unknown>
  request_id: string
}

export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export interface WellKnown {
  server_id: string
  canonical_url: string
  api_base: string
  protocol_version: number
  min_cli_protocol_version: number
  auth: { device_login: boolean }
}

export interface Capabilities {
  protocol_version: number
  minimum_cli_version: string
  features: string[]
}

export interface Actor {
  id: ID
  kind: 'human' | 'agent' | 'service'
  display_name: string
  bio: string
  avatar_url: string
}

/** 平台全局角色（round 33/34）：human ∈ admin/user；agent/service 固化 kind。
 * 仅 Me/AdminUser 响应携带；成员列表里的 Actor 不带（成员角色是 workspace 轴）。 */
export type PlatformRole = 'admin' | 'user' | 'agent' | 'service'

/** /admin/* 用户条目（仅平台 admin 可见）。 */
export interface AdminUser {
  id: ID
  kind: 'human' | 'agent' | 'service'
  platform_role: PlatformRole
  display_name: string
  email?: string
  disabled_at?: string
  created_at: string
}

export interface Workspace {
  id: ID
  name: string
  slug: string
  created_at: string
  updated_at?: string
}

// ---- Approvals（architecture §22，T-ws-6）----

export type ApprovalAction = 'membership.promote_owner'

export type ApprovalStatus =
  | 'requested'
  | 'approved'
  | 'rejected'
  | 'expired'
  | 'executed'

// 手工对齐 openapi Approval schema（服务端 workspace/approval.go approvalDTO）。
export interface Approval {
  id: ID
  workspace_id: ID
  action: ApprovalAction
  target_actor_id: ID
  status: ApprovalStatus
  requested_by: ID
  decided_by?: ID | null
  decided_at?: string | null
  expires_at: string
  created_at: string
}

export interface ApprovalPage extends Page<Approval> {}

// ---- Invitations（一次性邀请码注册，A5 / docs/registration.md）----

export type InvitationRole = 'viewer' | 'contributor' | 'maintainer'

export type InvitationStatus = 'invited' | 'redeemed' | 'revoked'

// 手工对齐 openapi Invitation schema（服务端 workspace/invitation.go invitationDTO）。
export interface Invitation {
  id: ID
  workspace_id: ID
  role: InvitationRole
  status: InvitationStatus
  created_by: ID
  created_at: string
  expires_at: string
  redeemed_by?: ID | null
  redeemed_at?: string | null
}

// 签发响应：Invitation 追加一次性 code 明文与拼好的注册链接（仅本次返回）。
export interface InvitationCreated extends Invitation {
  code: string
  invite_url: string
}

export interface InvitationPage extends Page<Invitation> {}

// ---- Tasks / Tags（手工对齐 openapi v2 Task/Tag/Lease schema）----
// v2（D15）：集合端点（children/task-search）行内恒填充 tags 与 children_count；
// lease 仍仅 get/claim 响应非空。

export type TaskStatus = 'open' | 'in_progress' | 'blocked' | 'review' | 'done' | 'cancelled'

export type TaskPriority = 'low' | 'normal' | 'high' | 'urgent'

export interface Lease {
  holder_actor_id: ID
  expires_at: string
  renewed_at?: string
}

export interface Tag {
  id: ID
  workspace_id: ID
  name: string
}

export interface Task {
  id: ID
  workspace_id: ID
  parent_id: ID | null
  title: string
  description: string
  status: TaskStatus
  priority: TaskPriority
  assignee_actor_id: ID | null
  revision: number
  tags: Tag[]
  children_count: number
  lease?: Lease | null
  created_at: string
  updated_at: string
}

// task-search 端点的条目 = Task + fuzzy 排序分（仅 fuzzy 查询时出现）。
export type TaskSearchHit = Task & { score?: number }

export interface Member {
  actor: Actor
  role: 'viewer' | 'contributor' | 'agent' | 'maintainer' | 'owner'
}

// SSE 事件 envelope（api/schemas/event.json）。
export interface EventEnvelope<T = Record<string, unknown>> {
  id: string
  type: string
  workspace_id?: string
  actor_id?: string
  occurred_at: string
  schema_version: number
  resource_revision?: number
  data: T
}
