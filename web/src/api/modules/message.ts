// message 作用域 API 模块（S7-4）。契约 = api/openapi.yaml messages 段：
// - 列表：GET /workspaces/{id}/messages（target_id / thread_id 过滤 + cursor，
//   最新在前 DESC；可见性：本 workspace 广播 + 自己收发的私信 + 本 workspace
//   任务的线程消息）；
// - 发送：POST /workspaces/{id}/messages（target = actor|workspace|task；
//   支持 Idempotency-Key，重试不得双发）；
// - 任务线程：GET /tasks/{id}/messages（时间正序 ASC 阅读序，cursor 取向随
//   排序方向 = 下一页更新）。
// 类型取自生成物 schema.d.ts（S6-2）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { components } from '@/api/schema'

export type MessageDto = components['schemas']['Message']
export type MessageSend = components['schemas']['MessageSend']
export type MessagePage = components['schemas']['MessagePage']
export type MessageTargetType = MessageSend['target']['type']

export interface MessageListParams {
  thread_id?: string
  target_id?: string
  limit?: number
  cursor?: string
}

export interface TaskThreadParams {
  limit?: number
  cursor?: string
}

// workspace 消息列表（广播 + 可见私信 + 任务消息，最新在前）。
// 服务端无 target_type 参数：类型（actor/workspace/task）过滤由调用方在
// 客户端按 target_type 分拣；精确到某个收件人用 target_id（服务端过滤）。
export function listMessages(
  workspaceId: string,
  params: MessageListParams = {},
  opts: CallOpts = {},
): Promise<MessagePage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/messages`, params),
    opts,
  )
}

// 发送消息。契约支持 Idempotency-Key（重试不得双发）：每次调用生成一个
// 新 key——GUI 的「重试」是用户重新点击（新意图），同一意图内的网络重试
// 由 axios 层外部处理时才需要调用方透传固定 key（当前无此路径，预留参数）。
export function sendMessage(
  workspaceId: string,
  payload: MessageSend,
  opts: CallOpts & { idempotencyKey?: string } = {},
): Promise<MessageDto> {
  const { idempotencyKey, ...call } = opts
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/messages`, {
    method: 'POST',
    body: payload,
    idempotencyKey: idempotencyKey ?? crypto.randomUUID(),
    ...call,
  })
}

// task thread 消息（时间正序阅读序；任务归属校验 404 TASK_NOT_FOUND）。
export function listTaskMessages(
  taskId: string,
  params: TaskThreadParams = {},
  opts: CallOpts = {},
): Promise<MessagePage> {
  return apiFetch(apiPath(`/api/v1/tasks/${encodeURIComponent(taskId)}/messages`, params), opts)
}
