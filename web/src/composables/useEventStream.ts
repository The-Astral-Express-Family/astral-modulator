// SSE 事件流组合式封装。传输层全部委托 @/api/sse.ts 的 subscribeEvents
// （Last-Event-ID 经 query 续传 + 指数退避重连，鉴权靠 Cookie），本层只管
// 生命周期（workspaceId 变化先退订再重订、作用域销毁时退订）与事件缓冲。

import { onScopeDispose, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { subscribeEvents } from '@/api/sse'
import type { EventEnvelope } from '@/api/types'

// 与 sse.ts onStateChange 回调的连接状态联合类型保持一致。
export type SseState = 'connecting' | 'open' | 'closed'

export interface UseEventStreamResult {
  events: Ref<EventEnvelope[]>
  state: Ref<SseState>
}

const MAX_EVENTS = 50

export function useEventStream(workspaceId: Ref<string | null>): UseEventStreamResult {
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
        events.value.unshift(env)
        if (events.value.length > MAX_EVENTS) events.value.pop()
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
