// workspace 作用域资源的 API 模块。路径必须与 api/openapi.yaml 一字不差；
// approvals 相关（architecture §22，T-ws-6）。

import { apiFetch, apiPath } from '../client'
import type {
  Approval,
  ApprovalPage,
  InvitationCreated,
  InvitationPage,
  Member,
  Page,
  Tag,
} from '../types'

export function listApprovals(
  workspaceId: string,
  params: { status?: string; limit?: number; cursor?: string } = {},
): Promise<ApprovalPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/approvals`, params),
  )
}

// decision: 'approve' 在同事务内执行 promote（仅 workspace owner 可裁决）；
// 返回裁决后的 Approval（status=executed|rejected）。
export function decideApproval(
  approvalId: string,
  decision: 'approve' | 'deny',
): Promise<Approval> {
  return apiFetch<Approval>(`/api/v1/approvals/${encodeURIComponent(approvalId)}/${decision}`, {
    method: 'POST',
  })
}

// workspace 级 tag 字典（展示用）；成员列表用于 actor id → 显示名映射。
export function listTags(workspaceId: string): Promise<Page<Tag>> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tags`)
}

export function listMembers(workspaceId: string): Promise<Page<Member>> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/members`)
}

// ---- Invitations（A5：签发/列表/撤销，授权=human session + manage_members）----

export function createInvitation(
  workspaceId: string,
  input: { role: 'viewer' | 'contributor' | 'maintainer'; expires_in?: number },
): Promise<InvitationCreated> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/invitations`, {
    method: 'POST',
    body: input,
  })
}

export function listInvitations(
  workspaceId: string,
  params: { status?: string } = {},
): Promise<InvitationPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/invitations`, params),
  )
}

/** 撤销邀请；对已关闭（redeemed/revoked）的撤销幂等 204。 */
export function revokeInvitation(invitationId: string): Promise<void> {
  return apiFetch(`/api/v1/invitations/${encodeURIComponent(invitationId)}/revoke`, {
    method: 'POST',
  })
}
