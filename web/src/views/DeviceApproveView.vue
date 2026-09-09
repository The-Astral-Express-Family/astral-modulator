<script setup lang="ts">
// Device Flow 人类审批页（architecture §8.2 / TODO.md A3）：
// CLI 发起登录后 verification_uri 指向 /device?code=XXXX-XXXX。
// 需先登录（未登录跳转 /login 并带 redirect）。
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { formatApiError } from '../api/client'
import {
  approveDeviceAuthorization,
  denyDeviceAuthorization,
  findDeviceAuthorization,
} from '../api/modules/auth'
import type { DeviceAuthorizationView } from '../api/modules/auth'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const route = useRoute()
const router = useRouter()

const view = ref<DeviceAuthorizationView | null>(null)
const manualCode = ref('')
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const busy = ref(false)

// 联调收尾（TODO.md §11 第 6 项）：pending 状态下每 5s 轮询一次，
// 请求在别处被处理/过期时页面自动跟进，不需要人工刷新。
const POLL_MS = 5_000
let pollTimer: ReturnType<typeof setInterval> | null = null

function stopPolling(): void {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function startPolling(code: string): void {
  stopPolling()
  pollTimer = setInterval(async () => {
    if (busy.value) return
    try {
      const fresh = await findDeviceAuthorization(code)
      view.value = fresh
      if (fresh.status !== 'pending') {
        stopPolling()
        notice.value =
          fresh.status === 'approved' || fresh.status === 'exchanged'
            ? '该请求已批准。'
            : `该请求已${fresh.status === 'expired' ? '过期' : '失效'}。`
      }
    } catch {
      // 查询失败（网络抖动等）不打断轮询；下一次循环重试。
    }
  }, POLL_MS)
}

function userCodeFromQuery(): string {
  return typeof route.query.code === 'string' ? route.query.code : ''
}

async function lookup(code: string): Promise<void> {
  error.value = null
  notice.value = null
  view.value = null
  stopPolling()
  if (!session.isLoggedIn) {
    void router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  try {
    view.value = await findDeviceAuthorization(code)
    if (view.value?.status === 'pending') startPolling(code)
  } catch (e) {
    error.value = formatApiError(e)
  }
}

async function decide(approve: boolean): Promise<void> {
  if (!view.value) return
  busy.value = true
  try {
    if (approve) {
      await approveDeviceAuthorization(view.value.id)
      notice.value = '已批准。请回到 CLI 终端，它会在几秒内完成登录。'
    } else {
      await denyDeviceAuthorization(view.value.id)
      notice.value = '已拒绝。该设备授权请求已终止。'
    }
    view.value = null
    stopPolling()
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  const code = userCodeFromQuery()
  if (code) void lookup(code)
})
onUnmounted(stopPolling)
</script>

<template>
  <h2>设备授权</h2>

  <div class="card" style="max-width: 520px">
    <p>
      输入 CLI 显示的代码：
      <input v-model="manualCode" placeholder="XXXX-XXXX" style="width: 160px" />
      <button :disabled="busy" @click="lookup(manualCode)">查询</button>
    </p>

    <p v-if="error" style="color: #b3261e">{{ error }}</p>
    <p v-if="notice" style="color: #1b7f3b">{{ notice }}</p>

    <div v-if="view && !notice">
      <p>
        请求代码：<code style="font-size: 1.4em">{{ view.user_code }}</code>
      </p>
      <p>客户端类型：{{ view.client_type }}</p>
      <p class="muted">
        请核对代码与 CLI 终端显示完全一致。批准后 CLI 将获得访问本服务器的凭证；
        如有疑问请拒绝。
      </p>
      <button :disabled="busy" @click="decide(true)">批准</button>
      <button :disabled="busy" style="margin-left: 8px" @click="decide(false)">拒绝</button>
    </div>
  </div>
</template>
