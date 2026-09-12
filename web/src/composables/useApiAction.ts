// 动作执行组合式封装：busy / 错误两态。
// 所有「点按钮调 API」场景共用：错误统一经 formatApiError 转单行文案；
// 成功提示不在此层（需要时用 vue-sonner toast 在调用点显式发）。

import { ref } from 'vue'
import type { Ref } from 'vue'
import { formatApiError } from '@/api/client'

export interface UseApiActionResult {
  busy: Ref<boolean>
  error: Ref<string | null>
  run(action: () => Promise<void>): Promise<boolean>
}

export function useApiAction(): UseApiActionResult {
  const busy = ref(false)
  const error = ref<string | null>(null)

  async function run(action: () => Promise<void>): Promise<boolean> {
    busy.value = true
    error.value = null
    try {
      await action()
      return true
    } catch (e) {
      error.value = formatApiError(e)
      return false
    } finally {
      busy.value = false
    }
  }

  return { busy, error, run }
}
