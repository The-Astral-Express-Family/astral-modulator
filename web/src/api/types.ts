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
