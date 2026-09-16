<!-- Workspace 总览：工作区信息 + presence 在线状态（S7-3）+ 实时事件流。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { RefreshCw } from '@lucide/vue'
import { getWorkspace } from '@/api/modules/core'
import { listPresence } from '@/api/modules/presence'
import type { Presence } from '@/api/modules/presence'
import type { Workspace } from '@/api/types'
import JsonBlock from '@/components/shared/JsonBlock.vue'
import KeyValue from '@/components/shared/KeyValue.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import InvitationsCard from '@/components/dashboard/InvitationsCard.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { useEventStream } from '@/composables/useEventStream'
import type { SseState } from '@/composables/useEventStream'
import { usePolling } from '@/composables/usePolling'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import { fmtTime } from '@/lib/format'

// 路由参数保持响应式：/workspaces/a → /workspaces/b 组件复用时正确重载。
const workspaceId = useWorkspaceId()
const workspace = ref<Workspace | null>(null)
// 事件缓冲与 SSE 生命周期（订阅 / 重订 / 退订）由 useEventStream 托管；
// presence 变更事件顺流而下触发防抖刷新（见下）。
const { events, state: sseState } = useEventStream(workspaceId, {
  onEvent: (env) => {
    if (env.type === 'actor.presence.changed') schedulePresenceReload()
  },
})

const SSE_VARIANTS: Record<SseState, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}

// ---- Presence 总览（S7-3；授权 = workspace:read，任何成员可读）----

const presence = ref<Presence[]>([])
const presenceLoaded = ref(false)
const presenceRefreshing = ref(false)

// 刷新策略：SSE actor.presence.changed 事件驱动（防抖合并）+ 60s 轮询兜底 +
// 手动刷新按钮。轮询兜底不可省：offline 是服务端读路径按 expires_at 派生的
// 状态（presence/module.go list），心跳 TTL 过期本身不产生任何事件，只有
// 重新拉取列表才能看到成员「下线」。
const PRESENCE_POLL_MS = 60_000
const { start: startPresencePolling } = usePolling(
  () => loadPresence({ silent: true }),
  PRESENCE_POLL_MS,
  { immediate: false, skip: () => document.hidden }, // 后台标签页不空转
)

let presenceDebounce: ReturnType<typeof setTimeout> | null = null

async function loadPresence(opts: { silent?: boolean } = {}): Promise<void> {
  if (presenceRefreshing.value) return
  presenceRefreshing.value = true
  try {
    const page = await listPresence(workspaceId.value, opts)
    presence.value = page.items ?? [] // next_cursor 恒 null（契约不分页），无翻页循环
    presenceLoaded.value = true
  } catch {
    // 首载/手动刷新失败由拦截器 toast；轮询失败静默保留旧值不刷屏。
  } finally {
    presenceRefreshing.value = false
  }
}

function schedulePresenceReload(): void {
  if (presenceDebounce) clearTimeout(presenceDebounce)
  presenceDebounce = setTimeout(() => {
    presenceDebounce = null
    void loadPresence({ silent: true })
  }, 500)
}

// 在线在前、离线在后；同组内按最近心跳倒序（「刚活跃」优先）。
const presenceSorted = computed(() =>
  [...presence.value].sort((a, b) => {
    const offline = (p: Presence): number => (p.state === 'offline' ? 1 : 0)
    if (offline(a) !== offline(b)) return offline(a) - offline(b)
    return b.last_heartbeat_at.localeCompare(a.last_heartbeat_at)
  }),
)
const onlineCount = computed(() => presence.value.filter((p) => p.state !== 'offline').length)
const offlineCount = computed(() => presence.value.length - onlineCount.value)

const PRESENCE_BADGE: Record<string, BadgeVariants['variant']> = {
  idle: 'secondary',
  planning: 'secondary',
  working: 'default',
  waiting: 'secondary',
  blocked: 'destructive',
  reviewing: 'secondary',
  offline: 'outline',
}

// 路由守卫保证登录态后本页才可到达；401（会话中途失效）由全局出口跳登录。
async function load(): Promise<void> {
  try {
    workspace.value = await getWorkspace(workspaceId.value)
  } catch {
    workspace.value = null // 404 不存在 / 403 无权限；UI 显示占位
  }
}

onMounted(() => {
  void load()
  void loadPresence() // 首载带全局错误提示；轮询/事件刷新走 silent
  startPresencePolling()
})

watch(workspaceId, () => {
  void load()
  void loadPresence()
})

onUnmounted(() => {
  if (presenceDebounce) clearTimeout(presenceDebounce)
})
</script>

<template>
  <PageHeader title="工作区总览" description="工作区基础信息与实时事件流。" />

  <Card>
    <CardHeader>
      <CardTitle>工作区信息</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <KeyValue label="ID" :value="workspaceId" />
      <template v-if="workspace">
        <KeyValue label="名称" :value="workspace.name" />
        <KeyValue label="Code" :value="workspace.slug" />
      </template>
      <div class="flex items-center justify-between gap-4">
        <span class="text-sm text-muted-foreground">SSE 状态</span>
        <Badge :variant="SSE_VARIANTS[sseState]">{{ sseState }}</Badge>
      </div>
      <div class="flex w-fit flex-wrap gap-2">
        <Button as-child>
          <RouterLink :to="`/workspaces/${workspaceId}/tasks`">任务树</RouterLink>
        </Button>
        <Button as-child variant="outline">
          <RouterLink :to="`/workspaces/${workspaceId}/approvals`">裁决队列</RouterLink>
        </Button>
        <Button as-child variant="outline">
          <RouterLink :to="`/workspaces/${workspaceId}/tags`">标签管理</RouterLink>
        </Button>
        <Button as-child variant="outline">
          <RouterLink :to="`/workspaces/${workspaceId}/messages`">消息</RouterLink>
        </Button>
      </div>
    </CardContent>
  </Card>

  <!-- 在线状态（S7-3）：事件驱动 + 轮询兜底 + 手动刷新（选择理由见 script 注释） -->
  <Card>
    <CardHeader>
      <CardTitle>在线状态</CardTitle>
      <CardAction>
        <Button
          variant="outline"
          size="sm"
          :disabled="presenceRefreshing"
          @click="loadPresence()"
        >
          <RefreshCw class="size-4" :class="presenceRefreshing ? 'animate-spin' : ''" />
          刷新
        </Button>
      </CardAction>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <p class="text-muted-foreground text-sm">
        在线 {{ onlineCount }} · 离线（最近）{{ offlineCount }}<template v-if="presenceLoaded">
          · 每 {{ Math.round(PRESENCE_POLL_MS / 1000) }} 秒自动刷新</template>
      </p>
      <ul v-if="presenceSorted.length" class="divide-y">
        <li
          v-for="p in presenceSorted"
          :key="p.actor_id"
          class="flex items-center gap-3 py-2 first:pt-0 last:pb-0"
        >
          <Badge :variant="PRESENCE_BADGE[p.state] ?? 'outline'">{{ p.state }}</Badge>
          <span class="min-w-0 flex-1 truncate text-sm">{{ p.display_name || p.actor_id }}</span>
          <span
            v-if="p.note"
            :title="p.note"
            class="text-muted-foreground hidden max-w-[40%] truncate text-xs md:block"
          >
            {{ p.note }}
          </span>
          <span class="text-muted-foreground shrink-0 text-xs">
            心跳 {{ fmtTime(p.last_heartbeat_at) }}
          </span>
        </li>
      </ul>
      <Empty v-else-if="presenceLoaded">
        <EmptyHeader>
          <EmptyTitle>暂无 presence 记录。</EmptyTitle>
          <EmptyDescription>
            成员（CLI / Agent）心跳后出现在此；记录过期后显示为 offline。
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    </CardContent>
  </Card>

  <!-- 邀请管理（A5）：无 manage_members 权限整卡自动隐藏（组件内 fail closed）。 -->
  <InvitationsCard :workspace-id="workspaceId" />

  <Empty v-if="!workspace">
    <EmptyHeader>
      <EmptyTitle>（无权访问或不存在）</EmptyTitle>
      <EmptyDescription>当前工作区不存在，或登录态不足以访问。</EmptyDescription>
    </EmptyHeader>
  </Empty>

  <Card>
    <CardHeader>
      <CardTitle>实时事件</CardTitle>
    </CardHeader>
    <CardContent>
      <JsonBlock v-if="events.length > 0" :value="events" />
      <Empty v-else>
        <EmptyHeader>
          <EmptyTitle>（等待事件…）</EmptyTitle>
          <EmptyDescription>订阅已建立，事件到达后展示在此（最多保留 50 条）。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    </CardContent>
  </Card>

  <!-- 文档活动流与冲突解决归 T13（S7-5/S7-6：DocumentsView / ConflictsView，
       含 R4 的 document.updated/conflict SSE 活动栏）——见 TODO.md §2 S7-5。 -->
</template>
