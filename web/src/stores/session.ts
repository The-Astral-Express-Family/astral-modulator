// 会话 store：登录态与服务器信息缓存。
// TODO(phase-1): 接入 HttpOnly Cookie session + /auth/me 轮询/事件驱动刷新；
// 未登录跳转 /device 或登录页。Device Flow 的 CLI 侧凭证不经过本 store
// （CLI 凭证存 OS Credential Store，与浏览器无关）。

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getCapabilities, getWellKnown } from '../api/modules/core'
import type { Capabilities, WellKnown } from '../api/types'

export const useSessionStore = defineStore('session', () => {
  const wellKnown = ref<WellKnown | null>(null)
  const capabilities = ref<Capabilities | null>(null)
  const connected = ref(false)

  async function loadServerInfo(): Promise<void> {
    wellKnown.value = await getWellKnown()
    capabilities.value = await getCapabilities()
    connected.value = true
  }

  return { wellKnown, capabilities, connected, loadServerInfo }
})
