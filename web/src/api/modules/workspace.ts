// workspace 作用域资源的 API 模块。路径必须与 api/openapi.yaml 一字不差；
// approvals 相关（architecture §22，T-ws-6）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type {
  Approval,
  ApprovalPage,
  InvitationCreated,
  InvitationPage,
  Member,
  Page,
  Tag,
} from '../types'

/** 待裁决列表：手动加载走全局 toast；15s 轮询传 silent（避免后端失联时刷屏）。 */
export function listApprovals(
  workspaceId: string,
  params: { status?: string; limit?: number; cursor?: string } = {},
  opts: CallOpts = {},
): Promise<ApprovalPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/approvals`, params),
    opts,
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
// 均为任务视图的后台字典拉取：失败不阻塞主视图（回退显示原始 id / 无联想）。
export function listTags(workspaceId: string): Promise<Page<Tag>> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tags`, { silent: true })
}

export function listMembers(workspaceId: string): Promise<Page<Member>> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/members`, { silent: true })
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

/** 列表加载即能力判断：403/404 → 调用方隐藏整卡（fail closed，不弹 toast）。 */
export function listInvitations(
  workspaceId: string,
  params: { status?: string } = {},
): Promise<InvitationPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/invitations`, params),
    { silent: true },
  )
}

/** 撤销邀请；对已关闭（redeemed/revoked）的撤销幂等 204。 */
export function revokeInvitation(invitationId: string): Promise<void> {
  return apiFetch(`/api/v1/invitations/${encodeURIComponent(invitationId)}/revoke`, {
    method: 'POST',
  })
}
