// 动作执行组合式封装：busy / 错误 / 成功提示三态。
// 所有「点按钮调 API」场景共用：错误统一经 formatApiError 转单行文案，
// action 返回 string 时写入 notice 并在 noticeTimeout 毫秒后自动清空。

import { onScopeDispose, ref } from 'vue'
import type { Ref } from 'vue'
import { formatApiError } from '@/api/client'

export interface UseApiActionOptions {
  /** notice 自动清空时长（毫秒），默认 3000 */
  noticeTimeout?: number
}

export interface UseApiActionResult {
  busy: Ref<boolean>
  error: Ref<string | null>
  notice: Ref<string | null>
  run(action: () => Promise<string | void>): Promise<boolean>
}

const DEFAULT_NOTICE_TIMEOUT = 3000

export function useApiAction(options?: UseApiActionOptions): UseApiActionResult {
  const busy = ref(false)
  const error = ref<string | null>(null)
  const notice = ref<string | null>(null)
  const noticeTimeout = options?.noticeTimeout ?? DEFAULT_NOTICE_TIMEOUT

  let noticeTimer: ReturnType<typeof setTimeout> | null = null

  function clearNoticeTimer(): void {
    if (noticeTimer !== null) {
      clearTimeout(noticeTimer)
      noticeTimer = null
    }
  }

  onScopeDispose(clearNoticeTimer)

  async function run(action: () => Promise<string | void>): Promise<boolean> {
    busy.value = true
    error.value = null
    notice.value = null
    clearNoticeTimer()
    try {
      const result = await action()
      if (typeof result === 'string') {
        notice.value = result
        noticeTimer = setTimeout(() => {
          noticeTimer = null
          notice.value = null
        }, noticeTimeout)
      }
      return true
    } catch (e) {
      error.value = formatApiError(e)
      return false
    } finally {
      busy.value = false
    }
  }

  return { busy, error, notice, run }
}
