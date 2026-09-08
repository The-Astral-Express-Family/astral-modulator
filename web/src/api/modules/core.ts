// 各资源 API 模块。路径必须与 api/openapi.yaml 一字不差。

import { apiFetch } from '../client'
import type { Capabilities, Page, Task, WellKnown, Workspace } from '../types'

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

export function listTasks(workspaceId: string, params: { status?: string; limit?: number; cursor?: string } = {}): Promise<Page<Task>> {
  const q = new URLSearchParams()
  if (params.status) q.set('status', params.status)
  if (params.limit) q.set('limit', String(params.limit))
  if (params.cursor) q.set('cursor', params.cursor)
  const qs = q.toString()
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tasks${qs ? `?${qs}` : ''}`)
}

// TODO(phase-3): searchTasks（regex+fuzzy 双参数）、tag proposal/confirm。
