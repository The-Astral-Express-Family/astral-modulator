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
import { TASKS_MOCK } from '../lib/mockMode'
import { DEMO_ACTOR, DEMO_CAPABILITIES, DEMO_WELL_KNOWN } from '../mocks/fixture'

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

// scheduleRenewal 到期前 60s 静默续期；失败不打断用户（Cookie 兜底）。
// 会话真正失效时由全局 401 出口接管（main.ts 注入 api/client，见 expireSession）。
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
  // 登录邮箱（human 只读展示；Me.email，agent/service 会话为 null）。
  const email = ref<string | null>(null)
  const wellKnown = ref<WellKnown | null>(null)
  const capabilities = ref<Capabilities | null>(null)
  const booted = ref(false)
  const bootError = ref<string | null>(null)
  let bootPromise: Promise<void> | null = null

  const isLoggedIn = computed(() => actor.value !== null)

  /** boot 与 login 共用的会话建立序列：Cookie 换 token 对 → 静默续期 → 拉身份。 */
  async function establishSession(): Promise<void> {
    const pair = await authApi.refreshWithCookie()
    setAccessToken(pair)
    scheduleRenewal()
    const me = await authApi.getMe()
    actor.value = me.actor
    email.value = me.email ?? null
  }

  /** 启动：well-known + 尝试 Cookie 续期恢复会话。并发安全：in-flight 复用同一 Promise。 */
  function boot(): Promise<void> {
    if (!bootPromise) bootPromise = runBoot()
    return bootPromise
  }

  /** boot 的实际执行体；内部各步骤均已捕获，不会 reject（缓存该 Promise 是安全的）。 */
  async function runBoot(): Promise<void> {
    try {
      wellKnown.value = await getWellKnown()
    } catch (e) {
      if (TASKS_MOCK) {
        // dev 演示兜底（后端不可达）：落演示登录态让任务视图可离线查看。
        applyDemoIdentity()
        booted.value = true
        return
      }
      bootError.value = formatApiError(e)
      booted.value = true
      return
    }
    try {
      await establishSession()
    } catch {
      actor.value = null // 未登录（正常路径）
    }
    if (!actor.value && TASKS_MOCK) {
      // dev 演示兜底（后端在线但未登录）：任务视图的 mock 数据不要求真实会话。
      // 后端在线时 wellKnown/capabilities 用真实值，仅身份为演示占位。
      applyDemoIdentity()
    }
    try {
      capabilities.value = await getCapabilities()
    } catch { /* 未登录时 capabilities 也可匿名访问；失败不阻塞 */ }
    booted.value = true
  }

  function applyDemoIdentity(): void {
    if (!wellKnown.value) wellKnown.value = DEMO_WELL_KNOWN
    actor.value = DEMO_ACTOR
    if (!capabilities.value) capabilities.value = DEMO_CAPABILITIES
  }

  // login/register 的响应本身就是 Me（含 actor/email），无需再 GET /auth/me：
  // 换 access token 一步即可（原 establishSession 的 3 请求收敛为 2）。
  async function adoptMe(me: { actor: Actor; email?: string }): Promise<void> {
    const pair = await authApi.refreshWithCookie()
    setAccessToken(pair)
    scheduleRenewal()
    actor.value = me.actor
    email.value = me.email ?? null
  }

  async function login(email: string, password: string): Promise<void> {
    await adoptMe(await authApi.login(email, password))
  }

  /** 注册（A5）：invite_code 非空走邀请兑换，为空则是 bootstrap；成功即建立会话。 */
  async function register(input: authApi.RegisterInput): Promise<void> {
    await adoptMe(await authApi.register(input))
  }

  async function logout(): Promise<void> {
    try {
      await authApi.logout()
    } finally {
      clearSession()
      actor.value = null
      email.value = null
    }
  }

  /** 本地会话失效（全局 401 出口调用）：清 token 与身份，不调服务端。 */
  function expireSession(): void {
    clearSession()
    actor.value = null
    email.value = null
  }

  return { actor, email, wellKnown, capabilities, booted, bootError, isLoggedIn, boot, login, register, logout, expireSession }
})
