// 轮询组合式封装：start 幂等（重复调用不叠加定时器）、逐轮可 skip、
// 随作用域（组件卸载 / effect scope 停止）自动 stop。

import { onScopeDispose } from 'vue'

export interface UsePollingOptions {
  /** 返回 true 时该轮跳过（如页面不可见、弹窗打开中） */
  skip?: () => boolean
  /** start 后是否立即执行一次，默认 true */
  immediate?: boolean
}

export interface UsePollingResult {
  start(): void
  stop(): void
}

export function usePolling(
  fn: () => Promise<void> | void,
  intervalMs: number,
  options?: UsePollingOptions,
): UsePollingResult {
  let timer: ReturnType<typeof setInterval> | null = null

  async function tick(): Promise<void> {
    if (options?.skip?.()) return
    await fn()
  }

  function start(): void {
    if (timer !== null) return // 幂等：已在轮询中则不叠加定时器
    if (options?.immediate !== false) void tick()
    timer = setInterval(() => {
      void tick()
    }, intervalMs)
  }

  function stop(): void {
    if (timer === null) return
    clearInterval(timer)
    timer = null
  }

  onScopeDispose(stop)

  return { start, stop }
}
