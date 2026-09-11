// SSE 订阅生命周期（视图间共享的单一实现）：
// - sseState 暴露连接状态；
// - subscribe() 按 workspaceId 当前值（重）订阅，换 workspace 时由视图的
//   load 流程重调，unsubscribe 幂等；
// - 组件卸载自动退订。
import { onUnmounted, ref, type Ref } from 'vue'
import { subscribeEvents } from '../api/sse'
import type { EventEnvelope } from '../api/types'

export type SseState = 'connecting' | 'open' | 'closed'

export function useWorkspaceEvents(
  workspaceId: Ref<string>,
  onEvent: (env: EventEnvelope) => void,
): { sseState: Ref<SseState>; subscribe: () => void } {
  const sseState = ref<SseState>('connecting')
  let unsubscribe: (() => void) | null = null

  function subscribe(): void {
    unsubscribe?.()
    unsubscribe = subscribeEvents({
      workspaceId: workspaceId.value,
      onEvent,
      onStateChange: (s) => (sseState.value = s),
    })
  }

  onUnmounted(() => unsubscribe?.())
  return { sseState, subscribe }
}
