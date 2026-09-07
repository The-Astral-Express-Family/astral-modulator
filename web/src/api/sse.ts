// SSE 订阅封装：Last-Event-ID 断线续传 + 指数退避重连。
// 浏览器 EventSource 不支持自定义头，鉴权依赖 Cookie session（同源）；
// 携带 token 的 SSE（若 phase-1 需要）改用 fetch 流式读取，届时在此扩展。

import type { EventEnvelope } from './types'

export interface SseOptions {
  workspaceId: string
  onEvent: (env: EventEnvelope) => void
  onStateChange?: (state: 'connecting' | 'open' | 'closed') => void
  /** 初始游标（上次收到的最后一个事件 id）；服务端 outbox 重放就绪后才真正生效 */
  lastEventId?: string
  maxBackoffMs?: number
}

export function subscribeEvents(opts: SseOptions): () => void {
  const { workspaceId, onEvent, onStateChange } = opts
  const maxBackoff = opts.maxBackoffMs ?? 15_000
  let closed = false
  let backoff = 1_000
  // EventSource 原生在重连时自动回带 Last-Event-ID；仅首连需要显式拼 query。
  // TODO(phase-4): 服务端 outbox 重放定稿后，确认 query 参数名与 window 约定，
  // 并处理 snapshot.required 事件（提示上层拉取快照）。
  const url = opts.lastEventId
    ? `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/events?last_event_id=${encodeURIComponent(opts.lastEventId)}`
    : `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/events`

  let es: EventSource | null = null

  const connect = () => {
    if (closed) return
    onStateChange?.('connecting')
    es = new EventSource(url)

    es.onopen = () => {
      backoff = 1_000
      onStateChange?.('open')
    }

    // 服务端把事件 type 写在 event: 行；对未显式匹配的 type 用 onmessage 兜底。
    es.onmessage = (ev) => dispatch(ev)
    // 逐个订阅已知事件 type（EventSource 需显式 addEventListener 才能收到带 event: 行的消息）
    for (const type of KNOWN_EVENT_TYPES) {
      es.addEventListener(type, (ev) => dispatch(ev as MessageEvent))
    }

    es.onerror = () => {
      es?.close()
      onStateChange?.('closed')
      if (!closed) {
        // 指数退避 + 抖动；EventSource 关闭后 native 重连游标丢失，
        // 最后一层兜底是把最近事件 id 拼进下次 URL。
        const delay = Math.min(backoff, maxBackoff) * (0.75 + Math.random() * 0.5)
        backoff = Math.min(backoff * 2, maxBackoff)
        setTimeout(connect, delay)
      }
    }
  }

  let lastSeenId = opts.lastEventId ?? ''
  const dispatch = (ev: MessageEvent) => {
    try {
      const env = JSON.parse(ev.data as string) as EventEnvelope
      if (env.id) lastSeenId = env.id
      onEvent(env)
    } catch {
      // 非 JSON data（如 keepalive 注释行不会进这里）；忽略
    }
  }
  // 保留 lastSeenId 供上层断线后取用（暂未在重连 URL 中使用，见上 TODO）。
  void lastSeenId

  connect()
  return () => {
    closed = true
    es?.close()
    onStateChange?.('closed')
  }
}

// 与服务端 internal/modules/event/types.go、api/schemas/event.json 保持同步。
// TODO(phase-2): 生成事件类型目录，消除三处手工同步。
export const KNOWN_EVENT_TYPES = [
  'workspace.member.changed',
  'actor.presence.changed',
  'task.created',
  'task.claimed',
  'task.lease.expired',
  'task.updated',
  'task.released',
  'document.updated',
  'document.conflict',
  'message.created',
  'security.credential.created',
  'security.credential.revoked',
  'human.override',
] as const
