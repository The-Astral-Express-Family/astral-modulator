// 任务视图实时事件流：mock 模式订阅内存事件总线（mocks/taskMock），真实模式走
// sse.ts 的 subscribeEvents。两种模式共用同一 onEvent 消费路径——snapshot.required、
// task.* 防抖重载等语义不因数据源分支。生命周期（workspaceId 变化重订 / 作用域
// 销毁退订）与 useEventStream 一致；不缓冲事件（任务视图即时消费）。

import { onScopeDispose, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { subscribeWorkspaceEvents } from '@/api/taskSource'
import type { EventEnvelope } from '@/api/types'
import type { SseState } from '@/composables/useEventStream'

export interface UseTaskLiveEventsResult {
  state: Ref<SseState>
}

export function useTaskLiveEvents(
  workspaceId: Ref<string>,
  onEvent: (env: EventEnvelope) => void,
): UseTaskLiveEventsResult {
  const state = ref<SseState>('connecting')
  let unsubscribe: (() => void) | null = null

  function subscribe(): void {
    unsubscribe?.()
    unsubscribe = null
    const id = workspaceId.value
    if (!id) return
    unsubscribe = subscribeWorkspaceEvents({
      workspaceId: id,
      onEvent,
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

  return { state }
}
