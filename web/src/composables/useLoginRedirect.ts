// 登录跳转 + redirect 参数消费。
// consumeRedirect 仅接受站内路径（以单个 '/' 开头且非 '//'），防开放重定向。

import { useRoute, useRouter } from 'vue-router'

function sanitizeRedirect(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  if (!raw.startsWith('/') || raw.startsWith('//')) return null
  return raw
}

export function useLoginRedirect(): {
  redirectToLogin(fullPath: string): void
  consumeRedirect(): string | null
} {
  const router = useRouter()
  const route = useRoute()

  function redirectToLogin(fullPath: string): void {
    void router.push({ path: '/login', query: { redirect: fullPath } })
  }

  function consumeRedirect(): string | null {
    return sanitizeRedirect(route.query.redirect)
  }

  return { redirectToLogin, consumeRedirect }
}
