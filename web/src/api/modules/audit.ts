// 审计查询 API 模块（S7-8）。契约 = api/openapi.yaml 两处 audit 端点：
// - workspace 轴：GET /workspaces/{id}/audit（audit:read；maintainer/owner），
//   结果按 workspace_id 收口；actor_id/action/outcome 过滤 + Limit/Cursor。
// - 平台轴：GET /admin/audit（RequireGlobal + platform:audit:read，admin
//   bundle 内），多一个 workspace_id 过滤；不带时含 workspace_id IS NULL 的
//   服务器级记录（认证失败等）。
// 两端点均为内联响应形状（非命名 Page schema），响应类型直接取生成类型
// operations[...]（presence.ts 同惯例）；条目形状共用 components.AuditEntry。
// 路径与契约一字不差。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { components, operations } from '../schema'

export type AuditEntry = components['schemas']['AuditEntry']
export type AuditOutcome = 'allowed' | 'denied' | 'error'
// 生成类型 items/next_cursor 未标 required（服务端实发恒有；按可选消费）。
export type AuditPage = operations['listAudit']['responses'][200]['content']['application/json']
export type AdminAuditPage = operations['listAdminAudit']['responses'][200]['content']['application/json']

export interface AuditFilterParams {
  actor_id?: string
  action?: string
  outcome?: AuditOutcome
  limit?: number
  cursor?: string
}

/** workspace 审计时间线（created_at 降序，游标 = 末行 id）。403 = 缺 audit:read。 */
export function listAudit(
  workspaceId: string,
  params: AuditFilterParams = {},
  opts: CallOpts = {},
): Promise<AuditPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/audit`, params),
    opts,
  )
}

/**
 * 平台级审计查询（含服务器级行）。workspace_id 留空 = 全部（含 NULL 行）。
 * 授权入口 RequireGlobal 无 404 分支：403 INSUFFICIENT_SCOPE 直接抛出，
 * 调用方（AuditView）据此转「无权访问」态（fail closed）。
 */
export function listAdminAudit(
  params: AuditFilterParams & { workspace_id?: string } = {},
  opts: CallOpts = {},
): Promise<AdminAuditPage> {
  return apiFetch(apiPath('/api/v1/admin/audit', params), opts)
}
