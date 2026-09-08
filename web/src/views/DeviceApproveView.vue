<script setup lang="ts">
// Device Flow 人类审批页（architecture §8.2 / TODO.md A3）：
// CLI 发起登录后 verification_uri 指向 /device?code=XXXX-XXXX。
// 需先登录（未登录跳转 /login 并带 redirect）。
import { onMounted, ref } from 'vue'
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

function userCodeFromQuery(): string {
  return typeof route.query.code === 'string' ? route.query.code : ''
}

async function lookup(code: string): Promise<void> {
  error.value = null
  notice.value = null
  view.value = null
  if (!session.isLoggedIn) {
    void router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  try {
    view.value = await findDeviceAuthorization(code)
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
