// workspace 作用域资源的 API 模块。路径必须与 api/openapi.yaml 一字不差；
// approvals 相关（architecture §22，T-ws-6）。

import { apiFetch } from '../client'
import type { Approval, ApprovalPage, Member, Page, Tag } from '../types'

export function listApprovals(
  workspaceId: string,
  params: { status?: string; limit?: number; cursor?: string } = {},
): Promise<ApprovalPage> {
  const q = new URLSearchParams()
  if (params.status) q.set('status', params.status)
  if (params.limit) q.set('limit', String(params.limit))
  if (params.cursor) q.set('cursor', params.cursor)
  const qs = q.toString()
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/approvals${qs ? `?${qs}` : ''}`,
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
