<script setup lang="ts">
// Device Flow 人类审批页（architecture §8.2 / TODO.md A3）：
// CLI 发起登录后 verification_uri 指向 /device?code=XXXX-XXXX。
// 需先登录 —— 由路由守卫统一拦截（未登录带 from 跳 /login，登录后原路返回）。
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  approveDeviceAuthorization,
  denyDeviceAuthorization,
  findDeviceAuthorization,
} from '@/api/modules/auth'
import type { DeviceAuthorizationView } from '@/api/modules/auth'
import { useApiAction } from '@/composables/useApiAction'
import { usePolling } from '@/composables/usePolling'
import DeviceAuthorizationCard from '@/components/device/DeviceAuthorizationCard.vue'
import DeviceCodeLookup from '@/components/device/DeviceCodeLookup.vue'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import StatusBadge from '@/components/shared/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'

const route = useRoute()

const view = ref<DeviceAuthorizationView | null>(null)
const manualCode = ref('')
// 终态结果（批准/拒绝/过期/失效）：持久展示，直到下一次查询。
const terminal = ref<{ status: DeviceAuthorizationView['status']; message: string } | null>(null)

const { busy, error, run } = useApiAction()

// 联调收尾（TODO.md §11 第 6 项）：pending 状态下每 5s 轮询一次，
// 请求在别处被处理/过期时页面自动跟进，不需要人工刷新。
const { start, stop } = usePolling(pollOnce, 5_000, { skip: () => busy.value })

// 轮询只应在 pending 状态运行：详情出现/状态变化时由 watch 控制 start/stop。
watch(
  () => view.value?.status,
  (status) => {
    if (status === 'pending') start()
    else stop()
  },
)

async function pollOnce(): Promise<void> {
  const code = view.value?.user_code
  if (!code) return
  try {
    const fresh = await findDeviceAuthorization(code)
    if (fresh.status !== 'pending') {
      view.value = null
      terminal.value = {
        status: fresh.status,
        message:
          fresh.status === 'approved' || fresh.status === 'exchanged'
            ? '该请求已批准。'
            : `该请求已${fresh.status === 'expired' ? '过期' : '失效'}`,
      }
    }
  } catch {
    // 查询失败（网络抖动等）不打断轮询；下一次循环重试。
  }
}

function userCodeFromQuery(): string {
  return typeof route.query.code === 'string' ? route.query.code : ''
}

async function lookup(code: string): Promise<void> {
  error.value = null
  terminal.value = null
  view.value = null
  stop()
  await run(async () => {
    view.value = await findDeviceAuthorization(code)
  })
}

async function decide(approve: boolean): Promise<void> {
  if (!view.value) return
  const id = view.value.id
  const ok = await run(async () => {
    if (approve) await approveDeviceAuthorization(id)
    else await denyDeviceAuthorization(id)
  })
  if (!ok) return
  view.value = null
  stop()
  terminal.value = approve
    ? { status: 'approved', message: '已批准。请回到 CLI 终端，它会在几秒内完成登录。' }
    : { status: 'denied', message: '已拒绝。该设备授权请求已终止。' }
}

onMounted(() => {
  const code = userCodeFromQuery()
  if (code) void lookup(code)
})
</script>

<template>
  <div class="flex w-full max-w-lg flex-col gap-4">
    <PageHeader title="设备授权" />

    <DeviceCodeLookup v-model="manualCode" :busy="busy" @lookup="lookup" />

    <ErrorAlert v-if="error" :message="error" />

    <DeviceAuthorizationCard
      v-if="view"
      :view="view"
      :busy="busy"
      @approve="decide(true)"
      @deny="decide(false)"
    />

    <Alert v-if="terminal">
      <div class="flex items-center gap-2">
        <StatusBadge :status="terminal.status" />
        <AlertDescription>{{ terminal.message }}</AlertDescription>
      </div>
    </Alert>
  </div>
</template>
