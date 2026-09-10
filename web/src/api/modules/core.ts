// 各资源 API 模块。路径必须与 api/openapi.yaml 一字不差。
// 目前只暴露 UI 已用到的调用，按需补充。

import { apiFetch, apiPath } from '../client'
import type { Capabilities, Page, WellKnown, Workspace } from '../types'

export function getWellKnown(): Promise<WellKnown> {
  return apiFetch('/.well-known/astral')
}

export function getCapabilities(): Promise<Capabilities> {
  return apiFetch('/api/v1/meta/capabilities')
}

export function listWorkspaces(
  params: { name?: string; limit?: number; cursor?: string } = {},
): Promise<Page<Workspace>> {
  return apiFetch(apiPath('/api/v1/workspaces', params))
}

export function getWorkspace(id: string): Promise<Workspace> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(id)}`)
}

// TODO: tag proposal/confirm 随 tag 审核流视图一并补充（任务列表/搜索已落地 task.ts）。
