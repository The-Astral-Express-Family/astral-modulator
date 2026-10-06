// task 作用域资源的 API 模块。路径必须与 api/openapi.yaml（v2）一字不差。
// v2 容器语义（TODO.md D15）：children 集合只回传直接子层（workspace 容器
// = 根层，task 容器 = 子层）；平面查询走 task-search（至少一个过滤条件）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { Page, Task, TaskPriority, TaskSearchHit } from '../types'

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
  blocked?: boolean
  blocked_by?: string
  limit?: number
  cursor?: string
}

// 平面查询（跨层级）；regex/fuzzy/tag/status/assignee/blocked/blocked_by
// 至少其一，否则 400（2.6.1 起依赖过滤单独即可满足）。
export function searchTasks(
  workspaceId: string,
  params: TaskSearchParams,
): Promise<Page<TaskSearchHit>> {
  return apiFetch(apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/task-search`, params))
}

// tags/children_count 集合响应也恒填充。
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

// 认领：事务内 assignee 条件更新（持有至释放/完成，无时间自动过期）；
// 竞争失败 409 TASK_ALREADY_CLAIMED。
export function claimTask(
  taskId: string,
  payload: { expected_revision: number },
  opts: CallOpts = {},
): Promise<{ task: Task }> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/claim`, {
    method: 'POST',
    body: payload,
    ...opts,
  })
}

// ---- 移动 / 重排（协议 2.4）----

export interface TaskMoveItem {
  task_id: string
  /** null = 移到根层 */
  parent_id: string | null
  expected_revision: number
  /** 目标兄弟序位（0 起，插入到该下标当前元素之前）；缺省 = 追加末尾 */
  position?: number
}

// 批量移动（同调用完成换父与兄弟内重排）：整批单事务全有或全无；
// 环/自挂/revision 冲突任一命中整批不生效。拖拽落点、右键「移动到」共用。
export function moveTasks(
  workspaceId: string,
  items: TaskMoveItem[],
  opts: CallOpts = {},
): Promise<{ items: Task[] }> {
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tasks/move`, {
    method: 'POST',
    body: { items },
    ...opts,
  })
}

// 释放认领：claimant 本人（或 task:override 强制）；清 assignee + in_progress→open。
export function releaseClaim(taskId: string, opts: CallOpts = {}): Promise<void> {
  return apiFetch(`/api/v1/tasks/${encodeURIComponent(taskId)}/claim`, { method: 'DELETE', ...opts })
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
