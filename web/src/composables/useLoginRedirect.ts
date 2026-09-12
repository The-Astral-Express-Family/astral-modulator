// 登录回跳：登录页消费 from 参数（守卫写入 to.fullPath）。
// 目标经 sanitizeRedirect 净化（防开放重定向 / 回跳循环），见 lib/redirect。

import { useRoute } from 'vue-router'
import { sanitizeRedirect } from '@/lib/redirect'

export function useLoginRedirect(): {
  consumeRedirect(): string | null
} {
  const route = useRoute()

  function consumeRedirect(): string | null {
    return sanitizeRedirect(route.query.from)
  }

  return { consumeRedirect }
}
