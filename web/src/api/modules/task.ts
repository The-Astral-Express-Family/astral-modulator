// task 作用域资源的 API 模块。路径必须与 api/openapi.yaml（v2）一字不差。
// v2 容器语义（TODO.md D15）：children 集合只回传直接子层（workspace 容器
// = 根层，task 容器 = 子层）；平面查询走 task-search（至少一个过滤条件）。

import { apiFetch, apiPath } from '../client'
import type { Page, Task, TaskSearchHit } from '../types'

export interface TaskFilterParams {
  status?: string
  tag?: string
  assignee?: string
  limit?: number
  cursor?: string
}

// workspace 容器的子任务集合 = 根层任务。
export function listWorkspaceChildren(
  workspaceId: string,
  params: TaskFilterParams = {},
): Promise<Page<Task>> {
  return apiFetch(apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/children`, params))
}

// task 容器的子任务集合 = 直接子层。
export function listTaskChildren(taskId: string, params: TaskFilterParams = {}): Promise<Page<Task>> {
  return apiFetch(apiPath(`/api/v1/tasks/${encodeURIComponent(taskId)}/children`, params))
}

export interface TaskSearchParams {
  regex?: string
  fuzzy?: string
  parent_id?: string
  tag?: string
  status?: string
  assignee?: string
  limit?: number
  cursor?: string
}

// 平面查询（跨层级）；regex/fuzzy/tag/status/assignee 至少其一，否则 400。
export function searchTasks(
  workspaceId: string,
  params: TaskSearchParams,
): Promise<Page<TaskSearchHit>> {
  return apiFetch(apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/task-search`, params))
}

// 详情响应填充 lease（tags/children_count 集合响应也带，详情另有 lease）。
export function getTask(taskId: string): Promise<Task> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}`)
}
