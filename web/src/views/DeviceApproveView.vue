<script setup lang="ts">
// Device Flow 人类审批页（architecture §8.2 / TODO.md A3）：
// CLI 发起登录后 verification_uri 指向 /device?code=XXXX-XXXX。
// 独立布局（不套 MainLayout，与 /login 同构）——CLI 拉起的浏览器窗口不携带应用外壳。
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
import PageHeader from '@/components/shared/PageHeader.vue'
import StatusBadge from '@/components/shared/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'

const route = useRoute()

const view = ref<DeviceAuthorizationView | null>(null)
const manualCode = ref('')
// code 来自 URL（verification_uri_complete 直达）时隐藏手动输码框——
// 页面只保留「确认请求 → 批准/拒绝」一步，对齐 VS Code 设备码登录体验。
const fromQueryCode = ref(false)
// 终态结果（批准/拒绝/过期/失效）：持久展示，直到下一次查询。
const terminal = ref<{ status: DeviceAuthorizationView['status']; message: string } | null>(null)

const { busy, run } = useApiAction()

// 联调收尾（TODO.md §11 第 6 项）：pending 状态下每 5s 轮询一次，
// 请求在别处被处理/过期时页面自动跟进，不需要人工刷新。
const { start, stop } = usePolling(pollOnce, 5_000, { skip: () => busy.value })

// 轮询只应在 pending 状态运行：详情出现/状态变化时由 watch 控制 start/stop。
// lookup 直接命中非 pending（expired/denied/exchanged）时同样进终态展示，
// 与「先 pending 再轮询发现」共用 enterTerminal 单一写入点。
watch(
  () => view.value?.status,
  (status) => {
    if (status === 'pending') start()
    else if (status) enterTerminal(status)
    else stop()
  },
)

// 终态文案单一映射：主动裁决与轮询发现（别处批准/拒绝/过期）共用同一展示。
const TERMINAL_MESSAGES: Record<DeviceAuthorizationView['status'], string> = {
  pending: '',
  approved: '该请求已批准。请回到 CLI 终端，它会在几秒内完成登录。',
  exchanged: '该请求已批准。请回到 CLI 终端，它会在几秒内完成登录。',
  denied: '该请求已拒绝。该设备授权请求已终止。',
  expired: '该请求已过期。该设备授权请求已终止。',
}

// 进入终态的单一写入点：清详情、落终态、停轮询。
function enterTerminal(status: DeviceAuthorizationView['status']): void {
  stop()
  view.value = null
  terminal.value = { status, message: TERMINAL_MESSAGES[status] }
}

async function pollOnce(): Promise<void> {
  const code = view.value?.user_code
  if (!code) return
  try {
    const fresh = await findDeviceAuthorization(code, { silent: true })
    if (fresh.status !== 'pending') enterTerminal(fresh.status)
  } catch {
    // 查询失败（网络抖动等）不打断轮询；下一次循环重试（silent：不刷屏）。
  }
}

function userCodeFromQuery(): string {
  return typeof route.query.code === 'string' ? route.query.code : ''
}

async function lookup(code: string): Promise<void> {
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
  enterTerminal(approve ? 'approved' : 'denied')
}

onMounted(() => {
  const code = userCodeFromQuery()
  if (code) {
    fromQueryCode.value = true
    void lookup(code)
  }
})
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="flex w-full max-w-lg flex-col gap-4">
      <PageHeader title="设备授权" />

      <DeviceCodeLookup v-if="!fromQueryCode" v-model="manualCode" :busy="busy" @lookup="lookup" />

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
  </div>
</template>
