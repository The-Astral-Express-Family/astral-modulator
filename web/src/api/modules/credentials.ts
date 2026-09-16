// S7-7 成员与凭证管理的 API 模块（MembersView 专属）。契约 =
// api/openapi.yaml workspaces 成员段 + agents 段 + approvals 创建：
// - PATCH /workspaces/{id}/members/{actor_id}（workspace:manage_members，
//   仅 owner）：role 变更；role=owner 被服务端 400 拒绝——owner 变更必须走
//   审批状态机（architecture §22），入口是 POST /workspaces/{id}/approvals
//   （action=membership.promote_owner，ApprovalCreate）。
// - GET/POST /workspaces/{id}/agents（agent:manage，maintainer/owner）。
// - POST /agents/{agent_id}/credentials（CredentialCreate {scopes, expires_at?}）
//   → CredentialIssued：明文 secret 仅本次响应返回一次（A4，服务端只存
//   sha256）；DELETE /agents/{agent_id}/credentials/{credential_id} 撤销
//   （联动断开其 SSE 连接）。契约无凭证列表端点——credential_id 只在签发
//   响应出现一次，撤销需凭该 id。
// - 签发请求体额外携带 workspace_id（服务端 createCredential 的授权分流
//   字段：非空 → 该 workspace 的 agent:manage；空 → platform:credentials:
//   manage 平台 admin 专属）。该字段未列入 openapi CredentialCreate
//   （additionalProperties 默认放行），本视图固定传当前 workspace，
//   使 maintainer/owner 可在其 workspace 内签发；偏离已登记任务卡 ③。
// 展示用的 listMembers / listApprovals / decideApproval 仍在 workspace.ts
// （该文件不在本任务卡清单内，不迁移）；本模块只承载 S7-7 管理面操作。
// 类型取自生成物 schema.d.ts（S6-2）；路径与契约一字不差。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { components } from '../schema'

export type MemberDto = components['schemas']['Member']
export type Role = components['schemas']['Role']
export type AgentDto = components['schemas']['Agent']
export type AgentCreate = components['schemas']['AgentCreate']
export type AgentPage = components['schemas']['AgentPage']
export type CredentialIssued = components['schemas']['CredentialIssued']
export type ApprovalDto = components['schemas']['Approval']

/** 调整成员 role（owner 除外——见文件头）。403 = 缺 workspace:manage_members。 */
export function updateMember(
  workspaceId: string,
  actorId: string,
  role: Exclude<Role, 'owner'>,
): Promise<MemberDto> {
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(actorId)}`,
    { method: 'PATCH', body: { role } },
  )
}

/**
 * 发起 owner 晋升审批（membership.promote_owner）。创建即 status=requested；
 * 裁决（approve 事务内执行晋升 / deny）走 workspace.ts 的 decideApproval，
 * 队列在 ApprovalsView。仅 owner 可发起（服务端 manage_members 校验）。
 */
export function requestOwnerPromotion(
  workspaceId: string,
  targetActorId: string,
): Promise<ApprovalDto> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/approvals`, {
    method: 'POST',
    body: { action: 'membership.promote_owner', target_actor_id: targetActorId },
  })
}

/** workspace 内 agent/service 列表（agent:manage）。silent 供调用方 fail-closed。 */
export function listAgents(
  workspaceId: string,
  opts: CallOpts = {},
): Promise<AgentPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/agents`),
    opts,
  )
}

/** 创建 agent/service actor（display_name 必填；kind 缺省 agent）。 */
export function createAgent(
  workspaceId: string,
  input: AgentCreate,
): Promise<AgentDto> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/agents`, {
    method: 'POST',
    body: input,
  })
}

/**
 * 签发 credential：明文 secret 仅本次响应返回（CredentialIssued），由
 * 调用方立即展示并仅保留 credential_id 供撤销。expires_at = RFC3339 或
 * 省略（永久）。workspace_id 分流见文件头注释。
 */
export function issueCredential(
  agentId: string,
  input: { scopes: string[]; expires_at?: string | null; workspace_id?: string },
): Promise<CredentialIssued> {
  const body: Record<string, unknown> = { scopes: input.scopes, workspace_id: input.workspace_id ?? '' }
  if (input.expires_at) body.expires_at = input.expires_at
  return apiFetch(`/api/v1/agents/${encodeURIComponent(agentId)}/credentials`, {
    method: 'POST',
    body,
  })
}

/** 撤销 credential（幂等语义：404 = 已不存在）。204 无响应体。 */
export function revokeCredential(agentId: string, credentialId: string): Promise<void> {
  return apiFetch(
    `/api/v1/agents/${encodeURIComponent(agentId)}/credentials/${encodeURIComponent(credentialId)}`,
    { method: 'DELETE' },
  )
}
