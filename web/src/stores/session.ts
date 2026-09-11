// 会话 store：Web 会话模型（architecture §8.4 / TODO.md D6）。
// refresh token 存 HttpOnly Cookie（JS 不可读）；access token 存内存，
// 到期前 60s 用 Cookie 静默续期（renewal timer）。access 通过
// currentAccessToken 注入 apiFetch 的 Bearer 头；即使续期间隙错过，
// 服务端对无 Bearer 请求也会回退 Cookie session 鉴权。

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { formatApiError, setAuthTokenProvider } from '../api/client'
import { getCapabilities, getWellKnown } from '../api/modules/core'
import * as authApi from '../api/modules/auth'
import type { Actor, Capabilities, WellKnown } from '../api/types'

// access token 内存态（模块级，刷新页面即失效——符合文档要求不进 JS 可读持久存储）。
let accessToken: string | null = null
let accessExpiresAt = 0
let renewalTimer: ReturnType<typeof setTimeout> | null = null

function currentAccessToken(): string | null {
  if (accessToken && Date.now() < accessExpiresAt - 30_000) return accessToken
  return null
}

// 注入 Bearer 来源（client 不反向依赖本模块）。
setAuthTokenProvider(currentAccessToken)

function setAccessToken(pair: { access_token: string; expires_in: number }): void {
  accessToken = pair.access_token
  accessExpiresAt = Date.now() + pair.expires_in * 1000
}

// scheduleRenewal 到期前 60s 静默续期；失败不打断用户（Cookie 兜底），
// 下一次 API 401 时由登录页流程处理。
function scheduleRenewal(): void {
  if (renewalTimer) clearTimeout(renewalTimer)
  if (!accessToken) return
  const delay = Math.max(accessExpiresAt - Date.now() - 60_000, 1_000)
  renewalTimer = setTimeout(() => {
    renewalTimer = null
    if (!accessToken) return
    authApi
      .refreshWithCookie()
      .then(setAccessToken)
      .then(scheduleRenewal)
      .catch(() => {
        // 续期失败（Cookie 失效/网络）：保留当前 token 至自然过期，
        // 后续请求走服务端 Cookie 回退或 401。
      })
  }, delay)
}

function clearSession(): void {
  if (renewalTimer) {
    clearTimeout(renewalTimer)
    renewalTimer = null
  }
  accessToken = null
}

export const useSessionStore = defineStore('session', () => {
  const actor = ref<Actor | null>(null)
  const wellKnown = ref<WellKnown | null>(null)
  const capabilities = ref<Capabilities | null>(null)
  const booted = ref(false)
  const bootError = ref<string | null>(null)

  const isLoggedIn = computed(() => actor.value !== null)

  /** boot 与 login 共用的会话建立序列：Cookie 换 token 对 → 静默续期 → 拉身份。 */
  async function establishSession(): Promise<void> {
    const pair = await authApi.refreshWithCookie()
    setAccessToken(pair)
    scheduleRenewal()
    const me = await authApi.getMe()
    actor.value = me.actor
  }

  /** 启动：well-known + 尝试 Cookie 续期恢复会话。 */
  async function boot(): Promise<void> {
    if (booted.value) return
    try {
      wellKnown.value = await getWellKnown()
    } catch (e) {
      bootError.value = formatApiError(e)
      booted.value = true
      return
    }
    try {
      await establishSession()
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
    await establishSession()
  }

  async function logout(): Promise<void> {
    try {
      await authApi.logout()
    } finally {
      clearSession()
      actor.value = null
    }
  }

  return { actor, wellKnown, capabilities, booted, bootError, isLoggedIn, boot, login, logout }
})
