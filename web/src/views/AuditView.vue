<!-- 审计时间线（S7-8）：workspace 轴（audit:read）+ 平台轴（platform:audit:read）
     双入口（tab 切换）。
     - admin 入口可见性：Me.platform_role === 'admin'（session.isPlatformAdmin；
       GlobalScopesFor 中 platform:audit:read 仅随 admin 授予，capabilities 端点
       不暴露 scope，platform_role 是客户端可得的权威信号——与 AdminUsersView
       导航收敛同源）。若角色信息过期，列表加载 403 时仍 fail closed 转无权态。
     - 过滤：actor（成员字典下拉）/ action（精确匹配文本）/ outcome（下拉），
       平台轴另加 workspace_id（留空 = 全部，含服务器级记录）。
       过滤为草稿态：点「查询」或回车统一生效，避免半输入状态打接口。
     - 分页：消费 next_cursor「加载更多」（id 降序游标，追加更早）。
     - 条目：时间 / actor / action / outcome / target / request_id；
       details 折叠展开为格式化 JSON（服务端已 redaction）。 -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listMembers } from '@/api/modules/workspace'
import { listAdminAudit, listAudit } from '@/api/modules/audit'
import type { AuditEntry, AuditOutcome } from '@/api/modules/audit'
import type { Member } from '@/api/types'
import { formatApiError } from '@/api/client'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import { useSessionStore } from '@/stores/session'
import { fmtTime } from '@/lib/format'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ShieldCheck, ScrollText } from '@lucide/vue'

const workspaceId = useWorkspaceId()
const session = useSessionStore()

const PAGE_LIMIT = 50

// ---- 双入口（tab）：平台轴仅平台管理员可见 ----

const mode = ref<'workspace' | 'admin'>('workspace')
const adminVisible = computed(() => session.isPlatformAdmin)

const MODES = computed(() =>
  [
    { value: 'workspace' as const, label: '工作区审计', icon: ScrollText, show: true },
    { value: 'admin' as const, label: '平台审计', icon: ShieldCheck, show: adminVisible.value },
  ].filter((m) => m.show),
)

// ---- 成员字典（actor id → 显示名；平台轴跨 workspace 时回退短 id）----

const members = ref<Member[]>([])
const memberNames = computed(() => {
  const m = new Map<string, string>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor.display_name)
  return m
})

const shortId = (id: string): string => id.slice(0, 12) + '…'
function actorLabel(id: string | null | undefined): string {
  if (!id) return '—'
  return memberNames.value.get(id) ?? shortId(id)
}

// ---- 过滤草稿（两轴各一份；「查询」统一生效）----

interface FilterDraft {
  actorId: string
  action: string
  outcome: '' | AuditOutcome
  workspaceId: string
}

const emptyDraft = (): FilterDraft => ({ actorId: '', action: '', outcome: '', workspaceId: '' })
const wsDraft = ref<FilterDraft>(emptyDraft())
const adminDraft = ref<FilterDraft>(emptyDraft())

function draftParams(d: FilterDraft): Record<string, string | number> {
  const params: Record<string, string | number> = { limit: PAGE_LIMIT }
  if (d.actorId) params.actor_id = d.actorId
  if (d.action.trim()) params.action = d.action.trim()
  if (d.outcome) params.outcome = d.outcome
  return params
}

const OUTCOME_OPTIONS = [
  { value: '', label: '全部结果' },
  { value: 'allowed', label: 'allowed' },
  { value: 'denied', label: 'denied' },
  { value: 'error', label: 'error' },
] as const

// ---- 工作区轴状态 ----

const wsItems = ref<AuditEntry[]>([])
const wsCursor = ref<string | null>(null)
const wsLoading = ref(false)
const wsLoaded = ref(false)
const wsError = ref<string | null>(null)

async function loadWorkspace(opts: { append?: boolean } = {}): Promise<void> {
  wsLoading.value = true
  try {
    const page = await listAudit(workspaceId.value, {
      ...draftParams(wsDraft.value),
      ...(opts.append && wsCursor.value ? { cursor: wsCursor.value } : {}),
    })
    const items = page.items ?? [] // 生成类型 items 可选（内联响应形状）
    wsItems.value = opts.append ? [...wsItems.value, ...items] : items
    wsCursor.value = page.next_cursor ?? null
    wsError.value = null
  } catch (e) {
    wsError.value = formatApiError(e)
  } finally {
    wsLoading.value = false
    wsLoaded.value = true
  }
}

// ---- 平台轴状态（首次切入懒加载）----

const adminItems = ref<AuditEntry[]>([])
const adminCursor = ref<string | null>(null)
const adminLoading = ref(false)
const adminLoaded = ref(false)
const adminError = ref<string | null>(null)

async function loadAdmin(opts: { append?: boolean } = {}): Promise<void> {
  adminLoading.value = true
  try {
    const page = await listAdminAudit({
      ...draftParams(adminDraft.value),
      ...(adminDraft.value.workspaceId.trim()
        ? { workspace_id: adminDraft.value.workspaceId.trim() }
        : {}),
      ...(opts.append && adminCursor.value ? { cursor: adminCursor.value } : {}),
    })
    const items = page.items ?? []
    adminItems.value = opts.append ? [...adminItems.value, ...items] : items
    adminCursor.value = page.next_cursor ?? null
    adminError.value = null
  } catch (e) {
    adminError.value = formatApiError(e)
  } finally {
    adminLoading.value = false
    adminLoaded.value = true
  }
}

watch(mode, (next) => {
  if (next === 'admin' && !adminLoaded.value && adminVisible.value) void loadAdmin()
})

// ---- 条目展开（details 折叠 JSON）----

const expandedIds = ref<Set<string>>(new Set())

function toggleDetails(id: string): void {
  const next = new Set(expandedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expandedIds.value = next
}

const fmtDetails = (entry: AuditEntry): string => JSON.stringify(entry.details ?? {}, null, 2)

// ---- 展示常量 ----

const OUTCOME_VARIANTS: Record<AuditOutcome, BadgeVariants['variant']> = {
  allowed: 'default',
  denied: 'destructive',
  error: 'secondary',
}
const OUTCOME_LABELS: Record<AuditOutcome, string> = {
  allowed: '通过',
  denied: '拒绝',
  error: '错误',
}

// ---- 生命周期 ----

onMounted(() => {
  void loadWorkspace()
  // 字典后台拉取（silent，失败回退短 id 显示）。
  listMembers(workspaceId.value)
    .then((page) => {
      members.value = page.items ?? []
    })
    .catch(() => {
      /* 显示退化为短 id，不阻塞 */
    })
})

watch(workspaceId, () => {
  wsItems.value = []
  wsCursor.value = null
  wsLoaded.value = false
  wsError.value = null
  wsDraft.value = emptyDraft()
  adminItems.value = []
  adminCursor.value = null
  adminLoaded.value = false
  adminError.value = null
  adminDraft.value = emptyDraft()
  members.value = []
  expandedIds.value = new Set()
  mode.value = 'workspace'
  void loadWorkspace()
  listMembers(workspaceId.value)
    .then((page) => {
      members.value = page.items ?? []
    })
    .catch(() => {
      /* 同上 */
    })
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader
        title="审计时间线"
        description="治理与安全动作的只追加记录：工作区轴含成员可见的全部操作，平台轴另含服务器级记录（认证失败等）。"
      />
      <div class="border-input flex rounded-lg border p-0.5">
        <Button
          v-for="m in MODES"
          :key="m.value"
          :variant="mode === m.value ? 'default' : 'ghost'"
          size="sm"
          @click="mode = m.value"
        >
          <component :is="m.icon" class="size-4" />
          {{ m.label }}
        </Button>
      </div>
    </div>

    <!-- 工作区轴 -->
    <Card v-if="mode === 'workspace'">
      <CardContent class="flex flex-col gap-3 p-4">
        <div class="flex flex-wrap items-end gap-2">
          <div class="w-52">
            <Label class="text-muted-foreground mb-1 block text-xs">操作者</Label>
            <Select v-model="wsDraft.actorId">
              <SelectTrigger size="sm" class="w-full">
                <SelectValue placeholder="全部操作者" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">全部操作者</SelectItem>
                <SelectItem v-for="mem in members" :key="mem.actor.id" :value="mem.actor.id">
                  {{ mem.actor.display_name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="w-52">
            <Label class="text-muted-foreground mb-1 block text-xs">动作（精确匹配）</Label>
            <Input
              v-model="wsDraft.action"
              size="sm"
              placeholder="如 task.claim / credential.create"
              @keydown.enter="loadWorkspace()"
            />
          </div>
          <div class="w-32">
            <Label class="text-muted-foreground mb-1 block text-xs">结果</Label>
            <Select v-model="wsDraft.outcome">
              <SelectTrigger size="sm" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in OUTCOME_OPTIONS" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button size="sm" :disabled="wsLoading" @click="loadWorkspace()">
            {{ wsLoading ? '查询中…' : '查询' }}
          </Button>
          <span class="text-muted-foreground ml-auto text-xs">
            已加载 {{ wsItems.length }} 条
          </span>
        </div>

        <div v-if="wsLoading && !wsItems.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 5" :key="i" class="h-16" :style="{ width: `${96 - i * 4}%` }" />
        </div>

        <Empty v-else-if="wsError" class="border">
          <EmptyHeader>
            <EmptyTitle>审计加载失败。</EmptyTitle>
            <EmptyDescription>{{ wsError }}</EmptyDescription>
          </EmptyHeader>
          <Button variant="outline" size="sm" @click="loadWorkspace()">重试</Button>
        </Empty>

        <template v-else-if="wsItems.length">
          <div
            v-for="entry in wsItems"
            :key="entry.id"
            class="flex flex-col gap-1.5 rounded-lg border p-3"
          >
            <div class="flex flex-wrap items-center gap-2 text-xs">
              <span class="text-sm font-medium">{{ actorLabel(entry.actor_id) }}</span>
              <Badge :variant="OUTCOME_VARIANTS[entry.outcome]">
                {{ OUTCOME_LABELS[entry.outcome] }}
              </Badge>
              <code class="font-mono">{{ entry.action }}</code>
              <span v-if="entry.target_type" class="text-muted-foreground">
                {{ entry.target_type }}
                <code v-if="entry.target_id" class="font-mono">{{ shortId(entry.target_id) }}</code>
              </span>
              <span class="text-muted-foreground ml-auto">{{ fmtTime(entry.created_at) }}</span>
            </div>
            <div class="flex items-center gap-2">
              <code class="text-muted-foreground font-mono text-xs">
                request_id: {{ entry.request_id ?? '—' }}
              </code>
              <Button
                variant="link"
                size="xs"
                class="h-auto p-0"
                @click="toggleDetails(entry.id)"
              >
                {{ expandedIds.has(entry.id) ? '收起详情' : '展开详情' }}
              </Button>
            </div>
            <pre
              v-if="expandedIds.has(entry.id)"
              class="bg-muted overflow-x-auto rounded-md p-2 font-mono text-xs"
            >{{ fmtDetails(entry) }}</pre>
          </div>
          <Button
            v-if="wsCursor"
            variant="outline"
            size="sm"
            class="self-start"
            :disabled="wsLoading"
            @click="loadWorkspace({ append: true })"
          >
            加载更早的记录
          </Button>
        </template>

        <Empty v-else-if="wsLoaded" class="border">
          <EmptyHeader>
            <EmptyTitle>没有审计记录。</EmptyTitle>
            <EmptyDescription>
              {{
                wsDraft.actorId || wsDraft.action || wsDraft.outcome
                  ? '当前过滤条件下无结果，试着放宽过滤。'
                  : '工作区还没有产生审计记录。'
              }}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>

    <!-- 平台轴（仅平台管理员） -->
    <Card v-else>
      <CardContent class="flex flex-col gap-3 p-4">
        <div class="flex flex-wrap items-end gap-2">
          <div class="w-52">
            <Label class="text-muted-foreground mb-1 block text-xs">workspace（留空含服务器级）</Label>
            <Input
              v-model="adminDraft.workspaceId"
              size="sm"
              placeholder="ws_…"
              @keydown.enter="loadAdmin()"
            />
          </div>
          <div class="w-52">
            <Label class="text-muted-foreground mb-1 block text-xs">操作者 ID</Label>
            <Input
              v-model="adminDraft.actorId"
              size="sm"
              placeholder="act_…（精确匹配）"
              @keydown.enter="loadAdmin()"
            />
          </div>
          <div class="w-52">
            <Label class="text-muted-foreground mb-1 block text-xs">动作（精确匹配）</Label>
            <Input
              v-model="adminDraft.action"
              size="sm"
              placeholder="如 auth.login"
              @keydown.enter="loadAdmin()"
            />
          </div>
          <div class="w-32">
            <Label class="text-muted-foreground mb-1 block text-xs">结果</Label>
            <Select v-model="adminDraft.outcome">
              <SelectTrigger size="sm" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in OUTCOME_OPTIONS" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button size="sm" :disabled="adminLoading" @click="loadAdmin()">
            {{ adminLoading ? '查询中…' : '查询' }}
          </Button>
          <span class="text-muted-foreground ml-auto text-xs">
            已加载 {{ adminItems.length }} 条
          </span>
        </div>

        <div v-if="adminLoading && !adminItems.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 5" :key="i" class="h-16" :style="{ width: `${96 - i * 4}%` }" />
        </div>

        <Empty v-else-if="adminError" class="border">
          <EmptyHeader>
            <EmptyTitle>平台审计加载失败。</EmptyTitle>
            <EmptyDescription>
              {{ adminError }}（platform:audit:read 仅平台管理员持有）
            </EmptyDescription>
          </EmptyHeader>
          <Button variant="outline" size="sm" @click="loadAdmin()">重试</Button>
        </Empty>

        <template v-else-if="adminItems.length">
          <div
            v-for="entry in adminItems"
            :key="entry.id"
            class="flex flex-col gap-1.5 rounded-lg border p-3"
          >
            <div class="flex flex-wrap items-center gap-2 text-xs">
              <span class="text-sm font-medium">{{ actorLabel(entry.actor_id) }}</span>
              <Badge :variant="OUTCOME_VARIANTS[entry.outcome]">
                {{ OUTCOME_LABELS[entry.outcome] }}
              </Badge>
              <code class="font-mono">{{ entry.action }}</code>
              <Badge v-if="entry.workspace_id" variant="outline" class="font-mono">
                {{ shortId(entry.workspace_id) }}
              </Badge>
              <Badge v-else variant="secondary">服务器级</Badge>
              <span v-if="entry.target_type" class="text-muted-foreground">
                {{ entry.target_type }}
                <code v-if="entry.target_id" class="font-mono">{{ shortId(entry.target_id) }}</code>
              </span>
              <span class="text-muted-foreground ml-auto">{{ fmtTime(entry.created_at) }}</span>
            </div>
            <div class="flex items-center gap-2">
              <code class="text-muted-foreground font-mono text-xs">
                request_id: {{ entry.request_id ?? '—' }}
              </code>
              <Button
                variant="link"
                size="xs"
                class="h-auto p-0"
                @click="toggleDetails(entry.id)"
              >
                {{ expandedIds.has(entry.id) ? '收起详情' : '展开详情' }}
              </Button>
            </div>
            <pre
              v-if="expandedIds.has(entry.id)"
              class="bg-muted overflow-x-auto rounded-md p-2 font-mono text-xs"
            >{{ fmtDetails(entry) }}</pre>
          </div>
          <Button
            v-if="adminCursor"
            variant="outline"
            size="sm"
            class="self-start"
            :disabled="adminLoading"
            @click="loadAdmin({ append: true })"
          >
            加载更早的记录
          </Button>
        </template>

        <Empty v-else-if="adminLoaded" class="border">
          <EmptyHeader>
            <EmptyTitle>没有平台审计记录。</EmptyTitle>
            <EmptyDescription>
              {{
                adminDraft.workspaceId || adminDraft.actorId || adminDraft.action || adminDraft.outcome
                  ? '当前过滤条件下无结果，试着放宽过滤。'
                  : '还没有平台级审计记录（含认证失败等服务器级行）。'
              }}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>
  </div>
</template>
