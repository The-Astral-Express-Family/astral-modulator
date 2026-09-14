// 动作执行组合式封装：busy 单态。
// 错误提示已统一收敛到 api/client 的响应拦截器（全局 toast），调用方不再
// 维护 error ref；需要按语义消化失败的场景改用模块函数的 silent 尾参。
// 成功提示不在此层（需要时用 vue-sonner toast 在调用点显式发）。

import { ref } from 'vue'
import type { Ref } from 'vue'

export interface UseApiActionResult {
  busy: Ref<boolean>
  run(action: () => Promise<void>): Promise<boolean>
}

export function useApiAction(): UseApiActionResult {
  const busy = ref(false)

  async function run(action: () => Promise<void>): Promise<boolean> {
    busy.value = true
    try {
      await action()
      return true
    } catch {
      // 失败已由全局拦截器 toast（或调用方经 silent 自行消化）。
      return false
    } finally {
      busy.value = false
    }
  }

  return { busy, run }
}
