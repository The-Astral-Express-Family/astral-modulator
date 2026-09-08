// 各资源 API 模块。路径必须与 api/openapi.yaml 一字不差。
// 目前只暴露 UI 已用到的调用；任务/搜索视图落地（phase-3）时按需补充。

import { apiFetch } from '../client'
import type { Capabilities, Page, WellKnown, Workspace } from '../types'

export function getWellKnown(): Promise<WellKnown> {
  return apiFetch('/.well-known/astral')
}

export function getCapabilities(): Promise<Capabilities> {
  return apiFetch('/api/v1/meta/capabilities')
}

export function listWorkspaces(params: { name?: string; limit?: number; cursor?: string } = {}): Promise<Page<Workspace>> {
  const q = new URLSearchParams()
  if (params.name) q.set('name', params.name)
  if (params.limit) q.set('limit', String(params.limit))
  if (params.cursor) q.set('cursor', params.cursor)
  const qs = q.toString()
  return apiFetch(`/api/v1/workspaces${qs ? `?${qs}` : ''}`)
}

export function getWorkspace(id: string): Promise<Workspace> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(id)}`)
}

// TODO(phase-3): tasks 列表/搜索（regex+fuzzy 双参数）、tag proposal/confirm 随任务视图一并补充。
