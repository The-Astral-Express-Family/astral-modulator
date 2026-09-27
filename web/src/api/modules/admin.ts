// 平台管理 API（round 34）：/admin/*。授权完全由服务端 RequireGlobal 判定，
// 403 在调用点按 fail-closed 处理（列表 403 → 无权访问态，见 AdminUsersView）。

import { apiFetch } from '../client'
import type { AdminUser, PlatformRole } from '../types'

/** 列表加载即能力判断：403/404 → 调用方转「无权访问」态（fail closed，不弹 toast）。 */
export function listAdminUsers(): Promise<{ items: AdminUser[] }> {
  return apiFetch('/api/v1/admin/users', { silent: true })
}

export function disableAdminUser(actorId: string): Promise<void> {
  return apiFetch(`/api/v1/admin/users/${actorId}/disable`, { method: 'POST' })
}

export function enableAdminUser(actorId: string): Promise<void> {
  return apiFetch(`/api/v1/admin/users/${actorId}/enable`, { method: 'POST' })
}

export function changeAdminUserRole(actorId: string, platformRole: Exclude<PlatformRole, 'agent' | 'service'>): Promise<AdminUser> {
  return apiFetch(`/api/v1/admin/users/${actorId}/role`, {
    method: 'POST',
    body: { platform_role: platformRole },
  })
}

// ---- 平台级注册邀请（00018）----

export interface PlatformInvitation {
  id: string
  status: 'invited' | 'redeemed' | 'revoked'
  created_by: string
  created_at: string
  expires_at: string
  redeemed_by?: string | null
  redeemed_at?: string | null
}

export interface PlatformInvitationCreated extends PlatformInvitation {
  /** 邀请码明文，仅签发响应返回一次 */
  code: string
  invite_url: string
}

/** 签发平台邀请：兑换后为普通 user，不入任何 workspace。 */
export function createPlatformInvitation(expiresIn: number): Promise<PlatformInvitationCreated> {
  return apiFetch('/api/v1/admin/invitations', {
    method: 'POST',
    body: { expires_in: expiresIn },
  })
}

/** 最近签发列表（silent：Dialog 内空态自行引导，不弹 toast）。 */
export function listPlatformInvitations(params?: {
  status?: 'invited' | 'redeemed' | 'revoked'
  limit?: number
}): Promise<{ items: PlatformInvitation[]; next_cursor: string | null }> {
  const q = new URLSearchParams()
  if (params?.status) q.set('status', params.status)
  if (params?.limit) q.set('limit', String(params.limit))
  const qs = q.toString()
  return apiFetch(`/api/v1/admin/invitations${qs ? `?${qs}` : ''}`, { silent: true })
}

export function revokePlatformInvitation(invitationId: string): Promise<void> {
  return apiFetch(`/api/v1/admin/invitations/${invitationId}/revoke`, { method: 'POST' })
}
