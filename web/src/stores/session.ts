// 会话 store：Web 会话模型（architecture §8.4 / TODO.md D6）。
// refresh token 存 HttpOnly Cookie（JS 不可读）；access token 存内存，
// 过期前用 Cookie 静默续期。access 通过 tokenRef 注入 apiFetch 的 Bearer 头。

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { setAuthTokenProvider } from '../api/client'
import { getCapabilities, getWellKnown } from '../api/modules/core'
import * as authApi from '../api/modules/auth'
import type { Actor, Capabilities, WellKnown } from '../api/types'

// access token 内存态（模块级，刷新页面即失效——符合文档要求不进 JS 可读持久存储）。
let accessToken: string | null = null
let accessExpiresAt = 0

export function currentAccessToken(): string | null {
  if (accessToken && Date.now() < accessExpiresAt - 30_000) return accessToken
  return null
}

// 注入 Bearer 来源（client 不反向依赖本模块）。
setAuthTokenProvider(currentAccessToken)

function setAccessToken(pair: { access_token: string; expires_in: number }): void {
  accessToken = pair.access_token
  accessExpiresAt = Date.now() + pair.expires_in * 1000
}

export const useSessionStore = defineStore('session', () => {
  const actor = ref<Actor | null>(null)
  const wellKnown = ref<WellKnown | null>(null)
  const capabilities = ref<Capabilities | null>(null)
  const booted = ref(false)
  const bootError = ref<string | null>(null)

  const isLoggedIn = computed(() => actor.value !== null)

  /** 启动：well-known + 尝试 Cookie 续期恢复会话。 */
  async function boot(): Promise<void> {
    if (booted.value) return
    try {
      wellKnown.value = await getWellKnown()
    } catch (e) {
      bootError.value = String(e)
      booted.value = true
      return
    }
    try {
      const pair = await authApi.refreshWithCookie()
      setAccessToken(pair)
      const me = await authApi.getMe()
      actor.value = me.actor
    } catch {
      actor.value = null // 未登录（正常路径）
    }
    try {
      capabilities.value = await getCapabilities()
    } catch { /* 未登录时 capabilities 也可匿名访问；失败不阻塞 */ }
    booted.value = true
  }

  async function login(email: string, password: string): Promise<void> {
    await authApi.login(email, password)
    const pair = await authApi.refreshWithCookie()
    setAccessToken(pair)
    const me = await authApi.getMe()
    actor.value = me.actor
  }

  async function logout(): Promise<void> {
    try {
      await authApi.logout()
    } finally {
      accessToken = null
      actor.value = null
    }
  }

  return { actor, wellKnown, capabilities, booted, bootError, isLoggedIn, boot, login, logout }
})
