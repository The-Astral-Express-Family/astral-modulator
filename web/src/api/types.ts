// API 类型定义 —— 手工对齐 api/openapi.yaml。
// TODO(phase-2): 换成 openapi-typescript 从 openapi.yaml 生成，消除手工同步漂移。
// 在此之前：改契约必须同时改 openapi.yaml 和本文件，并在 TODO.md 登记。

export type ID = string

export type ErrorCode =
  | 'AUTH_REQUIRED'
  | 'TOKEN_EXPIRED'
  | 'TOKEN_REVOKED'
  | 'INSUFFICIENT_SCOPE'
  | 'SERVER_NOT_FOUND'
  | 'WORKSPACE_NOT_FOUND'
  | 'WORKSPACE_ALREADY_BOUND'
  | 'TASK_NOT_FOUND'
  | 'TASK_ALREADY_CLAIMED'
  | 'TASK_LEASE_EXPIRED'
  | 'TAG_PROPOSAL_EXPIRED'
  | 'TAG_ALREADY_EXISTS'
  | 'REVISION_CONFLICT'
  | 'DOCUMENT_CONFLICT'
  | 'RATE_LIMITED'
  | 'CLIENT_VERSION_UNSUPPORTED'
  | 'VALIDATION_FAILED'
  | 'INTERNAL_ERROR'
  | 'NOT_IMPLEMENTED'

export interface ApiErrorBody {
  code: ErrorCode
  message: string
  retryable: boolean
  details?: Record<string, unknown>
  request_id: string
}

export interface ApiErrorEnvelope {
  error: ApiErrorBody
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

export interface Me {
  actor: Actor
  session?: { client_type: 'cli' | 'web'; expires_at?: string }
}

export interface Workspace {
  id: ID
  name: string
  slug: string
  created_at: string
  updated_at?: string
}

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
  description?: string
  status: TaskStatus
  priority?: TaskPriority
  assignee_actor_id?: ID | null
  revision: number
  tags?: Tag[]
  lease?: Lease | null
  created_at: string
  updated_at: string
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
