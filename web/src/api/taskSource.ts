// 任务数据源切换单点（round 25 移植自 round13 分支，契约改为 v2 容器语义）：
// 视图只 import taskApi / subscribeWorkspaceEvents，不感知 mock 或真实实现
// （两者签名一致）。mock 分支仅在 dev + VITE_TASKS_MOCK=1 时启用，生产构建经
// import.meta.env.DEV 常量折叠整支剔除。

import * as realTask from './modules/task'
import { listMembers as realListMembers, listTags as realListTags } from './modules/workspace'
import { subscribeEvents } from './sse'
import type { SseOptions } from './sse'
import type { CallOpts } from './client'
import type { Lease, Member, Page, Tag, Task, TaskSearchHit } from './types'
import { TASKS_MOCK } from '../lib/mockMode'
import { mockTaskApi, subscribeMockEvents } from '../mocks/taskMock'

export type { TaskCreatePayload, TaskFilterParams, TaskSearchParams, TaskUpdatePayload } from './modules/task'

export interface TaskApi {
  listWorkspaceChildren(
    workspaceId: string,
    params?: realTask.TaskFilterParams,
  ): Promise<Page<Task>>
  listTaskChildren(taskId: string, params?: realTask.TaskFilterParams): Promise<Page<Task>>
  searchTasks(workspaceId: string, params: realTask.TaskSearchParams): Promise<Page<TaskSearchHit>>
  getTask(taskId: string, opts?: CallOpts): Promise<Task>
  createRoot(workspaceId: string, payload: realTask.TaskCreatePayload): Promise<Task>
  createChild(taskId: string, payload: realTask.TaskCreatePayload): Promise<Task>
  updateTask(taskId: string, payload: realTask.TaskUpdatePayload, opts?: CallOpts): Promise<Task>
  claimTask(
    taskId: string,
    payload: { expected_revision: number; lease_seconds?: number },
    opts?: CallOpts,
  ): Promise<{ task: Task; lease: Lease }>
  renewLease(taskId: string, leaseSeconds?: number, opts?: CallOpts): Promise<Lease>
  releaseLease(taskId: string, opts?: CallOpts): Promise<void>
  attachTaskTag(taskId: string, tagId: string, expectedRevision?: number, opts?: CallOpts): Promise<Task>
  detachTaskTag(taskId: string, tagId: string, opts?: CallOpts): Promise<void>
  listTags(workspaceId: string): Promise<Page<Tag>>
  // 成员列表（assignee 显示名/头像）。任务视图在 mock 模式下也必须离线可用，
  // 所以经本接口走缝而非直接调 workspace 模块——否则真实 API 401 会触发全局登出。
  listMembers(workspaceId: string): Promise<Page<Member>>
}

// 与 sse.ts SseOptions 同形；mock 实现把模拟事件注入同一条消费路径。
export type SubscribeEventsFn = (opts: SseOptions) => () => void

export const taskApi: TaskApi = TASKS_MOCK
  ? mockTaskApi
  : { ...realTask, listTags: realListTags, listMembers: realListMembers }

export const subscribeWorkspaceEvents: SubscribeEventsFn = TASKS_MOCK
  ? subscribeMockEvents
  : subscribeEvents
