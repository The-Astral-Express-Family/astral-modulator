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
