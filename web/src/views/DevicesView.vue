<script setup lang="ts">
// 设备管理页（MainLayout 常规子页，URL /device 不变——CLI 的 verification_uri 仍指向这里）：
// ① 已登录设备列表（= 当前账号的活跃 sessions，含设备名/最近使用）+ 注销；
// ② 添加新设备：Device Flow 审批内嵌——CLI 显示设备码，在此查询并批准。
// 需先登录 —— 由路由守卫统一拦截（未登录带 from 跳 /login，登录后原路返回）。
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { RefreshCwIcon } from '@lucide/vue'
import { toast } from 'vue-sonner'
import {
  approveDeviceAuthorization,
  denyDeviceAuthorization,
  findDeviceAuthorization,
  listSessions,
  revokeSession,
} from '@/api/modules/auth'
import type { DeviceAuthorizationView, SessionView } from '@/api/modules/auth'
import { clearLastLocation } from '@/lib/lastLocation'
import { fmtRelative, fmtTime } from '@/lib/format'
import { useApiAction } from '@/composables/useApiAction'
import { usePolling } from '@/composables/usePolling'
import { useSessionStore } from '@/stores/session'
import DeviceAuthorizationCard from '@/components/device/DeviceAuthorizationCard.vue'
import DeviceCodeLookup from '@/components/device/DeviceCodeLookup.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import StatusBadge from '@/components/shared/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const { busy, run } = useApiAction()

// ---- 已登录设备列表 ----

const sessions = ref<SessionView[]>([])
const sessionsLoaded = ref(false)

async function loadSessions(): Promise<void> {
  if (session.isDemo) return // 演示身份不消费真实 API（与侧栏同规则）
  await run(async () => {
    sessions.value = await listSessions()
    sessionsLoaded.value = true
  })
}

// 浏览器 UA → 可读名；CLI 的 UA 即其产品标识，原文展示。
const KNOWN_BROWSERS: [RegExp, string][] = [
  [/Edg\//, 'Edge 浏览器'],
  [/OPR\//, 'Opera 浏览器'],
  [/Chrome\//, 'Chrome 浏览器'],
  [/Firefox\//, 'Firefox 浏览器'],
  [/Safari\//, 'Safari 浏览器'],
]

function deviceName(s: SessionView): string {
  const ua = s.user_agent.trim()
  if (s.client_type === 'cli') return ua || 'CLI 设备'
  for (const [re, name] of KNOWN_BROWSERS) {
    if (re.test(ua)) return name
  }
  return '浏览器'
}

// 注销：二次确认后 DELETE /auth/sessions/{id}（当前会话不走这里，见下）。
const revokeTarget = ref<SessionView | null>(null)
const revokingId = ref('')

function closeRevokeDialog(open: boolean): void {
  if (!open) revokeTarget.value = null
}

async function confirmRevoke(): Promise<void> {
  const target = revokeTarget.value
  if (!target) return
  revokeTarget.value = null
  revokingId.value = target.id
  try {
    await revokeSession(target.id)
    sessions.value = sessions.value.filter((s) => s.id !== target.id)
    toast.success('设备已注销')
  } finally {
    revokingId.value = ''
  }
}

// 当前会话的「注销」即登出：服务端撤当前 session + 清 Cookie，本地清理，
// 序列与侧栏 SessionBox 登出一致。
async function logoutCurrent(): Promise<void> {
  await session.logout()
  clearLastLocation()
  void router.push('/')
}

// ---- 添加新设备（Device Flow 审批） ----

const view = ref<DeviceAuthorizationView | null>(null)
const manualCode = ref('')
// code 来自 URL（verification_uri_complete 直达）时隐藏手动输码框——
// 页面只保留「确认请求 → 批准/拒绝」一步，对齐 VS Code 设备码登录体验。
const fromQueryCode = ref(false)
// 终态结果（批准/拒绝/过期/失效）：持久展示，直到下一次查询。
const terminal = ref<{ status: DeviceAuthorizationView['status']; message: string } | null>(null)

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
  // 批准后 CLI 数秒内完成兑换、新设备即出现在上方列表；兑换时机在 CLI 侧，
  // 这里只提示，用户可在列表点「刷新」确认。
  if (status === 'approved' || status === 'exchanged') toast.success('设备已批准，登录后出现在设备列表')
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
  void loadSessions()
  const code = userCodeFromQuery()
  if (code) {
    fromQueryCode.value = true
    void lookup(code)
  }
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader
      title="设备管理"
      description="查看已登录的设备与浏览器会话、注销不用的设备，或批准一台新设备登录"
    />

    <Card>
      <CardHeader class="flex flex-row items-start justify-between space-y-0">
        <div class="flex flex-col gap-1">
          <CardTitle>已登录设备</CardTitle>
          <CardDescription>「最近使用」为该设备最后一次通过鉴权的时间。</CardDescription>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          class="text-muted-foreground"
          aria-label="刷新设备列表"
          title="刷新"
          :disabled="busy"
          @click="loadSessions"
        >
          <RefreshCwIcon />
        </Button>
      </CardHeader>
      <CardContent>
        <p v-if="session.isDemo" class="text-sm text-muted-foreground">
          演示模式不加载真实设备列表。
        </p>
        <p v-else-if="!sessionsLoaded" class="text-sm text-muted-foreground">加载中…</p>
        <p v-else-if="!sessions.length" class="text-sm text-muted-foreground">
          还没有已登录的设备；在新设备上运行 <code>astral login</code> 并在下方批准即可添加。
        </p>
        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>设备</TableHead>
              <TableHead>最近使用</TableHead>
              <TableHead>登录于</TableHead>
              <TableHead class="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="s in sessions" :key="s.id">
              <TableCell>
                <div class="flex min-w-0 flex-col">
                  <span class="flex items-center gap-2">
                    <span class="max-w-72 truncate font-medium" :title="s.user_agent">
                      {{ deviceName(s) }}
                    </span>
                    <span v-if="s.client_type === 'cli'" class="text-muted-foreground text-xs">CLI</span>
                  </span>
                  <span class="text-muted-foreground text-xs">{{ s.remote_addr || '—' }}</span>
                </div>
              </TableCell>
              <TableCell :title="s.last_used_at ? fmtTime(s.last_used_at) : undefined">
                {{ s.last_used_at ? fmtRelative(s.last_used_at) : '从未使用' }}
              </TableCell>
              <TableCell>{{ fmtTime(s.created_at) }}</TableCell>
              <TableCell class="text-right">
                <Button
                  v-if="s.current"
                  variant="outline"
                  size="sm"
                  @click="logoutCurrent"
                >
                  退出登录
                </Button>
                <Button
                  v-else
                  variant="outline"
                  size="sm"
                  :disabled="revokingId === s.id"
                  @click="revokeTarget = s"
                >
                  注销
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle>添加新设备</CardTitle>
        <CardDescription>
          在新设备上运行 <code>astral login</code>，CLI 会显示一个 8 位设备码；在此输入并批准，即把该设备加入你的账号。
        </CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
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
      </CardContent>
    </Card>

    <AlertDialog :open="revokeTarget !== null" @update:open="closeRevokeDialog">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>确认注销「{{ revokeTarget ? deviceName(revokeTarget) : '' }}」？</AlertDialogTitle>
          <AlertDialogDescription>
            注销后该设备上的登录将立即失效；如需继续使用，须重新登录。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="confirmRevoke">注销</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
