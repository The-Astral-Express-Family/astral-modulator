// 各资源 API 模块。路径必须与 api/openapi.yaml 一字不差。
// 目前只暴露 UI 已用到的调用，按需补充。

import { apiFetch, apiPath } from '../client'
import type { Capabilities, Page, WellKnown, Workspace } from '../types'

/** boot 探测：失败由 App.vue 的 bootError 横幅承载，不重复弹 toast。 */
export function getWellKnown(): Promise<WellKnown> {
  return apiFetch('/.well-known/astral', { silent: true })
}

/** boot 探测：匿名可访问，失败不阻塞也不提示。 */
export function getCapabilities(): Promise<Capabilities> {
  return apiFetch('/api/v1/meta/capabilities', { silent: true })
}

export function listWorkspaces(
  params: { name?: string; limit?: number; cursor?: string } = {},
): Promise<Page<Workspace>> {
  return apiFetch(apiPath('/api/v1/workspaces', params))
}

export function getWorkspace(id: string): Promise<Workspace> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(id)}`)
}

// WorkspaceCreate（openapi）：name 必填（1-200），slug 省略时服务端由 name 派生
// （仅 [a-z0-9]，纯非 ASCII 名派生不出 → 400，需调用方提供）。
export function createWorkspace(input: { name: string; slug?: string }): Promise<Workspace> {
  return apiFetch('/api/v1/workspaces', { method: 'POST', body: input })
}

// TODO: tag proposal/confirm 随 tag 审核流视图一并补充（任务列表/搜索已落地 task.ts）。
