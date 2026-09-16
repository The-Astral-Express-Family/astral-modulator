// presence 作用域资源的 API 模块。路径必须与 api/openapi.yaml 一字不差。
// 列表信封 = openapi listPresence 的内联形状（items + 恒 null 的 next_cursor，
// 字段未标 required；服务端实现直接省略该键——见 presence/module.go list，
// 按 workspace 成员规模有界不分页，消费侧按可选处理，无需翻页循环）。

import { apiFetch } from '../client'
import type { CallOpts } from '../client'
import type { components, operations } from '../schema'

export type Presence = components['schemas']['Presence']
// 响应形状直接取生成类型（无泛型 Page<T> 可复用；presence 本就不分页）。
export type PresenceList = operations['listPresence']['responses'][200]['content']['application/json']

/** workspace 在线状态总览（授权 = workspace:read，任何成员可读）。 */
export function listPresence(workspaceId: string, opts: CallOpts = {}): Promise<PresenceList> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/presence`, opts)
}
