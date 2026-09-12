// SSE 事件流组合式封装（视图间共享的单一实现）。传输层全部委托
// @/api/sse.ts 的 subscribeEvents（Last-Event-ID 经 query 续传 + 指数退避
// 重连，鉴权靠 Cookie），本层只管生命周期（workspaceId 变化先退订再重订、
// 作用域销毁时退订）、事件缓冲与消费方 tap。

import { onScopeDispose, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { subscribeEvents } from '@/api/sse'
import type { SseState } from '@/api/sse'
import type { EventEnvelope } from '@/api/types'

export type { SseState }

export interface UseEventStreamOptions {
  /** 每事件 tap（与缓冲无关；缓冲照常维护），如 task.* 触发防抖重载 */
  onEvent?: (env: EventEnvelope) => void
  /** 内存事件缓冲容量，默认 50 */
  maxEvents?: number
}

export interface UseEventStreamResult {
  events: Ref<EventEnvelope[]>
  state: Ref<SseState>
}

const DEFAULT_MAX_EVENTS = 50

export function useEventStream(
  workspaceId: Ref<string | null>,
  options: UseEventStreamOptions = {},
): UseEventStreamResult {
  const { onEvent, maxEvents = DEFAULT_MAX_EVENTS } = options
  const events = ref<EventEnvelope[]>([])
  const state = ref<SseState>('connecting')
  let unsubscribe: (() => void) | null = null

  function subscribe(): void {
    unsubscribe?.()
    unsubscribe = null
    events.value = [] // 工作区切换后不保留旧流事件
    const id = workspaceId.value
    if (!id) return
    unsubscribe = subscribeEvents({
      workspaceId: id,
      onEvent: (env) => {
        onEvent?.(env)
        events.value.unshift(env)
        if (events.value.length > maxEvents) events.value.pop()
      },
      onStateChange: (s) => {
        state.value = s
      },
    })
  }

  watch(workspaceId, subscribe, { immediate: true })

  onScopeDispose(() => {
    unsubscribe?.()
    unsubscribe = null
  })

  return { events, state }
}
