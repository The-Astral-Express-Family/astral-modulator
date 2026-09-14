// task 作用域资源的 API 模块。路径必须与 api/openapi.yaml（v2）一字不差。
// v2 容器语义（TODO.md D15）：children 集合只回传直接子层（workspace 容器
// = 根层，task 容器 = 子层）；平面查询走 task-search（至少一个过滤条件）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { Lease, Page, Task, TaskPriority, TaskSearchHit } from '../types'

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
// 详情面板的写路径统一 silent（冲突/租约过期由面板按语义提示），回源刷新亦 silent；
// 用户主动点开的详情查询走全局 toast。
export function getTask(taskId: string, opts: CallOpts = {}): Promise<Task> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}`, opts)
}

// ---- 写操作（round 25 任务视图 UI 移植；端点在 v2 中未变，集合寻址见上）----

export interface TaskCreatePayload {
  title: string
  description?: string
  priority?: TaskPriority
  /** 已存在 tag 名（按规范化名解析；任一不存在 → 404，整体不创建） */
  tags?: string[]
}

// v2（D15）：创建 = 向容器 POST children；workspace 容器 = 根任务，task 容器 = 子任务。
export function createRoot(workspaceId: string, payload: TaskCreatePayload): Promise<Task> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/children`, {
    method: 'POST',
    body: payload,
  })
}

export function createChild(taskId: string, payload: TaskCreatePayload): Promise<Task> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/children`, {
    method: 'POST',
    body: payload,
  })
}

export interface TaskUpdatePayload {
  expected_revision: number
  title?: string
  description?: string
  status?: string
  priority?: string
  assignee_actor_id?: string | null
}

// 乐观并发：expected_revision 不符 → 409 REVISION_CONFLICT（details.current_revision）。
export function updateTask(
  taskId: string,
  payload: TaskUpdatePayload,
  opts: CallOpts = {},
): Promise<Task> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}`, {
    method: 'PATCH',
    body: payload,
    ...opts,
  })
}

// 认领：事务内条件更新抢租约；竞争失败 409 TASK_ALREADY_CLAIMED。
export function claimTask(
  taskId: string,
  payload: { expected_revision: number; lease_seconds?: number },
  opts: CallOpts = {},
): Promise<{ task: Task; lease: Lease }> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/claim`, {
    method: 'POST',
    body: payload,
    ...opts,
  })
}

export function renewLease(
  taskId: string,
  leaseSeconds?: number,
  opts: CallOpts = {},
): Promise<Lease> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/lease/renew`, {
    method: 'POST',
    body: leaseSeconds ? { lease_seconds: leaseSeconds } : {},
    ...opts,
  })
}

// 释放：仅 holder（或 task:override）；清租约 + 清 assignee + in_progress→open。
export function releaseLease(taskId: string, opts: CallOpts = {}): Promise<void> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/lease`, { method: 'DELETE', ...opts })
}

// attach 幂等：已关联时返回当前 Task、不 bump revision。
export function attachTaskTag(
  taskId: string,
  tagId: string,
  expectedRevision?: number,
  opts: CallOpts = {},
): Promise<Task> {
  return apiFetch(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/tags/${encodeURIComponent(tagId)}`,
    { method: 'PUT', body: expectedRevision ? { expected_revision: expectedRevision } : {}, ...opts },
  )
}

// detach 幂等：未关联时 204、不 bump revision。
export function detachTaskTag(taskId: string, tagId: string, opts: CallOpts = {}): Promise<void> {
  return apiFetch(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/tags/${encodeURIComponent(tagId)}`,
    { method: 'DELETE', ...opts },
  )
}
