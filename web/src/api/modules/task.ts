// task 作用域资源的 API 模块。路径必须与 api/openapi.yaml 一字不差。
// list：结构化过滤 + cursor 分页（无 regex/fuzzy）；search：regex/fuzzy 语义
// 见 openapi（至少其一），条目带可选 score。

import { apiFetch } from '../client'
import type { Page, Task, TaskSearchHit } from '../types'

export interface TaskListParams {
  parent_id?: string
  status?: string
  assignee?: string
  limit?: number
  cursor?: string
}

export function listTasks(workspaceId: string, params: TaskListParams = {}): Promise<Page<Task>> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tasks?${buildQuery(params)}`)
}

export interface TaskSearchParams {
  regex?: string
  fuzzy?: string
  parent_id?: string
  tag?: string
  status?: string
  limit?: number
  cursor?: string
}

export function searchTasks(
  workspaceId: string,
  params: TaskSearchParams,
): Promise<Page<TaskSearchHit>> {
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tasks/search?${buildQuery(params)}`,
  )
}

// 详情响应填充 tags/lease（list/search 省略，D11）。
export function getTask(taskId: string): Promise<Task> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}`)
}

function buildQuery(params: object): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  return q.toString()
}
