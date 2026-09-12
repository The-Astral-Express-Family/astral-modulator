// 站内重定向目标的净化：路由守卫与登录页共用。
// 仅接受以单个 '/' 开头的站内路径（拒绝 '//' 防开放重定向）；
// '/login' 自身会造成「已登录 → login → 回跳 login」循环，同样拒绝。

export function sanitizeRedirect(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  if (!raw.startsWith('/') || raw.startsWith('//')) return null
  if (raw === '/login' || raw.startsWith('/login?')) return null
  return raw
}

// 401 出口与路由守卫共用的登录跳转目标（带 from 回跳）。
export function loginLocation(from: string): { path: string; query: { from: string } } {
  return { path: '/login', query: { from } }
}
