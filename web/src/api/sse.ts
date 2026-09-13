// SSE 订阅封装：Last-Event-ID 断线续传 + 指数退避重连。
// 浏览器 EventSource 不支持自定义头，鉴权依赖 Cookie session（同源）；
// 携带 token 的 SSE（若需要）改用 fetch 流式读取，届时在此扩展。

import type { EventEnvelope } from './types'

/** SSE 连接状态（订阅方统一引用此处的单一声明）。 */
export type SseState = 'connecting' | 'open' | 'closed'

export interface SseOptions {
  workspaceId: string
  onEvent: (env: EventEnvelope) => void
  onStateChange?: (state: SseState) => void
  /** 初始游标（上次收到的最后一个事件 id），后续重连自动携带最新游标 */
  lastEventId?: string
  maxBackoffMs?: number
}

export function subscribeEvents(opts: SseOptions): () => void {
  const { workspaceId, onEvent, onStateChange } = opts
  const maxBackoff = opts.maxBackoffMs ?? 15_000
  let closed = false
  let backoff = 1_000
  let lastSeenId = opts.lastEventId ?? ''

  // 我们主动 close 后重建 EventSource，native 重连及其 Last-Event-ID 回带
  // 随之失效；因此每次（重）连都把最近事件 id 拼进 query（服务端两个通道等价）。
  const buildUrl = (): string => {
    const base = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/events`
    return lastSeenId ? `${base}?last_event_id=${encodeURIComponent(lastSeenId)}` : base
  }

  let es: EventSource | null = null

  const connect = () => {
    if (closed) return
    onStateChange?.('connecting')
    es = new EventSource(buildUrl())

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
        // 指数退避 + 抖动；重连时经 buildUrl 携带 lastSeenId 断点续传。
        const delay = Math.min(backoff, maxBackoff) * (0.75 + Math.random() * 0.5)
        backoff = Math.min(backoff * 2, maxBackoff)
        setTimeout(connect, delay)
      }
    }
  }

  const dispatch = (ev: MessageEvent) => {
    try {
      const env = JSON.parse(ev.data as string) as EventEnvelope
      if (env.id) lastSeenId = env.id
      onEvent(env)
    } catch {
      // 非 JSON data（如 keepalive 注释行不会进这里）；忽略
    }
  }

  connect()
  return () => {
    closed = true
    es?.close()
    onStateChange?.('closed')
  }
}

// 与服务端 internal/modules/event/types.go、api/schemas/event.json 三方同步
// （服务端 TestEventTypesSync 契约门覆盖）。
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
  'security.invite.created',
  'security.invite.revoked',
  'security.invite.redeemed',
  'tag.created',
  'tag.renamed',
  'tag.deleted',
  'snapshot.required',
  'human.override',
] as const
