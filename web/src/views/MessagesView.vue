<!-- 消息视图（S7-4）：两视角 —— workspace 动态流（广播 + 可见私信 + 任务消息，
     最新在前）与任务线程（时间正序阅读序）。
     - 目标过滤：类型（actor/workspace/task）为客户端分拣（契约列表端点无
       target_type 参数）；定向到某收件人走 target_id（服务端过滤）。
     - 发送：target 三选（广播 / 私信成员 / 搜索任务）+ body（1–20000 字符）；
       sendMessage 携带 Idempotency-Key（契约：重试不得双发）。
     - 分页：两视角均消费 next_cursor「加载更多」；feed 追加更早、thread 追加
       更新（服务端排序方向不同）。
     - SSE message.created：防抖静默重拉当前视角首页（游标重置；T11 任务树的
       reloadVisible 同精神）。可见性提示与服务端 list 分支一致：广播=成员可见，
       私信=仅收发双方，任务消息=本 workspace 任务。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import { Megaphone, MessagesSquare, Search, Send } from '@lucide/vue'
import { listMessages, listTaskMessages, sendMessage } from '@/api/modules/message'
import type { MessageDto, MessageTargetType } from '@/api/modules/message'
import { getTask, searchTasks } from '@/api/modules/task'
import { listMembers } from '@/api/modules/workspace'
import type { Member, TaskSearchHit } from '@/api/types'
import { useSessionStore } from '@/stores/session'
import { useEventStream } from '@/composables/useEventStream'
import type { SseState } from '@/composables/useEventStream'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import PageHeader from '@/components/shared/PageHeader.vue'
import TaskStatusBadge from '@/components/tasks/TaskStatusBadge.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'

const workspaceId = useWorkspaceId()
const session = useSessionStore()

const PAGE_LIMIT = 50

// ---- SSE 实时刷新 ----

const { state: sseState } = useEventStream(workspaceId, { onEvent: onSseEvent })
let reloadTimer: ReturnType<typeof setTimeout> | null = null

const SSE_VARIANTS: Record<SseState, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}

function onSseEvent(env: { type: string }): void {
  if (env.type !== 'message.created') return
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => {
    reloadTimer = null
    if (mode.value === 'feed') void loadFeed({ silent: true })
    else if (threadTask.value) void loadThread({ silent: true })
  }, 800)
}

onUnmounted(() => {
  if (reloadTimer) clearTimeout(reloadTimer)
})

// ---- 成员字典（发送者显示名 + 私信对象选择）----

const members = ref<Member[]>([])
const memberNames = computed(() => {
  const m = new Map<string, string>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor.display_name)
  return m
})

const shortId = (id: string): string => id.slice(0, 12) + '…'
function memberName(id: string | null | undefined): string {
  if (!id) return '—'
  return memberNames.value.get(id) ?? shortId(id)
}

// ---- 视角切换 ----

const mode = ref<'feed' | 'thread'>('feed')
const MODES = [
  { value: 'feed', label: '动态流', icon: Megaphone },
  { value: 'thread', label: '任务线程', icon: MessagesSquare },
] as const

// ---- 视角一：动态流（最新在前；target_id 服务端过滤 + 类型客户端分拣）----

const feedItems = ref<MessageDto[]>([])
const feedCursor = ref<string | null>(null)
const feedLoading = ref(false)
const feedLoaded = ref(false)
const typeFilter = ref<'all' | MessageTargetType>('all')
const actorFilter = ref('') // '' = 全部对象

const TYPE_CHIPS = [
  { value: 'all', label: '全部' },
  { value: 'workspace', label: '广播' },
  { value: 'actor', label: '私信' },
  { value: 'task', label: '任务' },
] as const

async function loadFeed(opts: { append?: boolean; silent?: boolean } = {}): Promise<void> {
  feedLoading.value = true
  try {
    const page = await listMessages(
      workspaceId.value,
      {
        limit: PAGE_LIMIT,
        ...(actorFilter.value ? { target_id: actorFilter.value } : {}),
        ...(opts.append && feedCursor.value ? { cursor: feedCursor.value } : {}),
      },
      { silent: opts.silent },
    )
    const items = page.items ?? [] // 生成类型 items 可选（allOf 合并形态）
    feedItems.value = opts.append ? [...feedItems.value, ...items] : items
    feedCursor.value = page.next_cursor
  } catch {
    // 失败已由全局拦截器 toast（silent 时为 SSE 防抖刷新，不打扰）。
  } finally {
    feedLoading.value = false
    feedLoaded.value = true
  }
}

const visibleFeedItems = computed(() =>
  typeFilter.value === 'all'
    ? feedItems.value
    : feedItems.value.filter((m) => m.target_type === typeFilter.value),
)

// ---- 视角二：任务线程（时间正序阅读序）----

const threadTask = ref<TaskSearchHit | null>(null)
const threadItems = ref<MessageDto[]>([])
const threadCursor = ref<string | null>(null)
const threadLoading = ref(false)
const threadLoaded = ref(false)

async function loadThread(opts: { append?: boolean; silent?: boolean } = {}): Promise<void> {
  const task = threadTask.value
  if (!task) return
  threadLoading.value = true
  try {
    const page = await listTaskMessages(
      task.id,
      {
        limit: PAGE_LIMIT,
        ...(opts.append && threadCursor.value ? { cursor: threadCursor.value } : {}),
      },
      { silent: opts.silent },
    )
    const items = page.items ?? [] // 生成类型 items 可选（allOf 合并形态）
    threadItems.value = opts.append ? [...threadItems.value, ...items] : items
    threadCursor.value = page.next_cursor
  } catch {
    // 失败已由全局拦截器 toast。
  } finally {
    threadLoading.value = false
    threadLoaded.value = true
  }
}

// 任务搜索选择器（线程视角与发送表单各一份实例，共用实现）。
function useTaskPicker() {
  const query = ref('')
  const hits = ref<TaskSearchHit[]>([])
  const searching = ref(false)
  async function search(): Promise<void> {
    const q = query.value.trim()
    if (!q) {
      toast.error('输入关键词后再搜索任务。')
      return
    }
    searching.value = true
    try {
      const page = await searchTasks(workspaceId.value, { fuzzy: q, limit: 10 })
      hits.value = page.items
    } catch {
      // 失败已由全局拦截器 toast。
    } finally {
      searching.value = false
    }
  }
  return { query, hits, searching, search }
}

const threadPicker = useTaskPicker()

function selectThreadTask(hit: TaskSearchHit): void {
  threadPicker.query.value = ''
  threadPicker.hits.value = []
  threadTask.value = hit
  threadItems.value = []
  threadCursor.value = null
  threadLoaded.value = false
  // 发送面顺势对准该任务（仍可改发广播/私信）。
  sendTarget.value = 'task'
  sendTaskId.value = hit.id
  sendTaskTitle.value = hit.title
  void loadThread()
}

// 动态流里的任务消息 → 跳线程视角（标题经 getTask 补齐，失败退短 id）。
function openThreadFromFeed(msg: MessageDto): void {
  if (msg.target_type !== 'task') return
  mode.value = 'thread'
  threadTask.value = { ...emptyTask(), id: msg.target_id, title: '加载中…' }
  threadItems.value = []
  threadCursor.value = null
  threadLoaded.value = false
  void loadThread()
  getTask(msg.target_id, { silent: true })
    .then((t) => {
      if (threadTask.value?.id === t.id) threadTask.value = t
    })
    .catch(() => {
      if (threadTask.value?.id === msg.target_id) threadTask.value.title = shortId(msg.target_id)
    })
}

function emptyTask(): TaskSearchHit {
  return {
    id: '',
    workspace_id: workspaceId.value,
    parent_id: null,
    title: '',
    description: '',
    status: 'open',
    priority: 'normal',
    assignee_actor_id: null,
    revision: 1,
    tags: [],
    children_count: 0,
    created_at: '',
    updated_at: '',
  }
}

// ---- 发送表单（target 三选 + body）----

const sendTarget = ref<MessageTargetType>('workspace')
const sendActorId = ref('')
const sendTaskId = ref('')
const sendTaskTitle = ref('')
const body = ref('')
const sending = ref(false)
const sendPicker = useTaskPicker()

const bodyValid = computed(() => {
  const trimmed = body.value.trim()
  return trimmed.length >= 1 && trimmed.length <= 20000
})

function selectSendTask(hit: TaskSearchHit): void {
  sendPicker.query.value = ''
  sendPicker.hits.value = []
  sendTaskId.value = hit.id
  sendTaskTitle.value = hit.title
}

function send(): void {
  if (!bodyValid.value || sending.value) return
  let target: { type: MessageTargetType; id: string }
  if (sendTarget.value === 'workspace') {
    target = { type: 'workspace', id: workspaceId.value }
  } else if (sendTarget.value === 'actor') {
    if (!sendActorId.value) {
      toast.error('私信需要先选择收件人。')
      return
    }
    target = { type: 'actor', id: sendActorId.value }
  } else {
    if (!sendTaskId.value) {
      toast.error('任务消息需要先搜索并选择任务。')
      return
    }
    target = { type: 'task', id: sendTaskId.value }
  }
  sending.value = true
  sendMessage(workspaceId.value, { target, body: body.value.trim() })
    .then((sent) => {
      toast.success('消息已发送。')
      body.value = ''
      // 本地即时补显（去重；SSE 防抖刷新随后对齐）。线程只在已到末页时追加
      // （未到末页时中间还有未加载的更新消息，直接追加会乱序——交给刷新对齐）。
      if (mode.value === 'feed' && matchesFeedFilters(sent) && !feedItems.value.some((m) => m.id === sent.id)) {
        feedItems.value = [sent, ...feedItems.value]
      } else if (
        mode.value === 'thread' &&
        threadCursor.value === null &&
        sent.target_type === 'task' &&
        sent.target_id === threadTask.value?.id &&
        !threadItems.value.some((m) => m.id === sent.id)
      ) {
        threadItems.value = [...threadItems.value, sent]
      }
    })
    .catch(() => {
      // 失败已由全局拦截器 toast。
    })
    .finally(() => {
      sending.value = false
    })
}

function matchesFeedFilters(msg: MessageDto): boolean {
  if (typeFilter.value !== 'all' && msg.target_type !== typeFilter.value) return false
  if (actorFilter.value && msg.target_id !== actorFilter.value) return false
  return true
}

// ---- 展示辅助 ----

function targetLabel(msg: MessageDto): string {
  if (msg.target_type === 'workspace') return '广播'
  if (msg.target_type === 'actor') return `私信 → ${memberName(msg.target_id)}`
  return `任务 ${shortId(msg.target_id)}`
}

const isOwn = (msg: MessageDto): boolean => msg.sender_id === session.actor?.id
const fmtTime = (iso: string): string => new Date(iso).toLocaleString()

// ---- 生命周期 ----

onMounted(() => {
  void loadFeed()
  listMembers(workspaceId.value)
    .then((page) => {
      members.value = page.items
    })
    .catch(() => {
      /* 字典失败不阻塞：显示退化为短 id（silent，不弹 toast） */
    })
})

watch(mode, () => {
  if (mode.value === 'feed' && !feedLoaded.value) void loadFeed()
  if (mode.value === 'thread' && !threadLoaded.value && threadTask.value) void loadThread()
})

watch(actorFilter, () => {
  if (mode.value === 'feed') void loadFeed()
})

watch(workspaceId, () => {
  feedItems.value = []
  feedCursor.value = null
  feedLoaded.value = false
  typeFilter.value = 'all'
  actorFilter.value = ''
  threadTask.value = null
  threadItems.value = []
  threadCursor.value = null
  threadLoaded.value = false
  sendTarget.value = 'workspace'
  sendActorId.value = ''
  sendTaskId.value = ''
  sendTaskTitle.value = ''
  body.value = ''
  members.value = []
  void loadFeed()
  listMembers(workspaceId.value).then((page) => {
    members.value = page.items
  }).catch(() => {
    /* 同上，静默降级 */
  })
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader
        title="消息"
        description="workspace 动态流与任务线程：广播成员可见，私信仅收发双方，任务消息随任务可见。"
      />
      <div class="flex items-center gap-2">
        <Badge :variant="SSE_VARIANTS[sseState]">SSE {{ sseState }}</Badge>
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
    </div>

    <!-- 视角一：动态流 -->
    <Card v-if="mode === 'feed'">
      <CardContent class="flex flex-col gap-3 p-4">
        <div class="flex flex-wrap items-center gap-2">
          <div class="border-input flex gap-0.5 rounded-lg border p-0.5">
            <Button
              v-for="chip in TYPE_CHIPS"
              :key="chip.value"
              :variant="typeFilter === chip.value ? 'default' : 'ghost'"
              size="xs"
              @click="typeFilter = chip.value"
            >
              {{ chip.label }}
            </Button>
          </div>
          <Select v-model="actorFilter">
            <SelectTrigger size="sm" class="w-56">
              <SelectValue placeholder="全部对象" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部对象</SelectItem>
              <SelectItem v-for="mem in members" :key="mem.actor.id" :value="mem.actor.id">
                {{ mem.actor.display_name }}
              </SelectItem>
            </SelectContent>
          </Select>
          <span class="text-muted-foreground ml-auto text-xs">
            已加载 {{ visibleFeedItems.length }} 条（过滤为客户端分拣）
          </span>
        </div>

        <div v-if="feedLoading && !feedItems.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 5" :key="i" class="h-16" :style="{ width: `${96 - i * 4}%` }" />
        </div>

        <template v-else-if="visibleFeedItems.length">
          <div
            v-for="msg in visibleFeedItems"
            :key="msg.id"
            class="flex flex-col gap-1 rounded-lg border p-3"
            :class="isOwn(msg) ? 'bg-primary/5' : ''"
          >
            <div class="flex flex-wrap items-center gap-2 text-xs">
              <span class="text-sm font-medium">{{ memberName(msg.sender_id) }}</span>
              <span v-if="isOwn(msg)" class="text-muted-foreground">（我）</span>
              <Badge variant="outline">{{ targetLabel(msg) }}</Badge>
              <Button
                v-if="msg.target_type === 'task'"
                variant="link"
                size="xs"
                class="h-auto p-0"
                @click="openThreadFromFeed(msg)"
              >
                查看线程
              </Button>
              <span class="text-muted-foreground ml-auto">{{ fmtTime(msg.created_at) }}</span>
            </div>
            <p class="text-sm break-all whitespace-pre-wrap">{{ msg.body }}</p>
          </div>
          <Button
            v-if="feedCursor"
            variant="outline"
            size="sm"
            class="self-start"
            :disabled="feedLoading"
            @click="loadFeed({ append: true })"
          >
            加载更早的消息
          </Button>
        </template>

        <Empty v-else-if="feedLoaded" class="border">
          <EmptyHeader>
            <EmptyTitle>没有可见消息。</EmptyTitle>
            <EmptyDescription>
              {{
                typeFilter !== 'all' || actorFilter
                  ? '当前过滤条件下无结果，试着放宽过滤。'
                  : '还没有消息：用下方表单发一条广播试试。'
              }}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>

    <!-- 视角二：任务线程 -->
    <Card v-else>
      <CardContent class="flex flex-col gap-3 p-4">
        <div class="flex flex-wrap items-end gap-2">
          <div class="min-w-56 flex-1">
            <Label class="text-muted-foreground mb-1 block text-xs">按关键词找任务</Label>
            <div class="relative">
              <Search class="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
              <Input
                v-model="threadPicker.query.value"
                placeholder="任务标题关键词…"
                class="pl-8"
                @keydown.enter="threadPicker.search()"
              />
            </div>
          </div>
          <Button size="sm" :disabled="threadPicker.searching.value" @click="threadPicker.search()">
            {{ threadPicker.searching.value ? '搜索中…' : '搜索任务' }}
          </Button>
        </div>

        <div
          v-if="threadPicker.hits.value.length"
          class="flex flex-col gap-1 rounded-lg border border-dashed p-2"
        >
          <button
            v-for="hit in threadPicker.hits.value"
            :key="hit.id"
            type="button"
            class="hover:bg-muted/60 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm"
            @click="selectThreadTask(hit)"
          >
            <TaskStatusBadge :status="hit.status" />
            <span class="min-w-0 flex-1 truncate">{{ hit.title }}</span>
            <code class="text-muted-foreground hidden shrink-0 font-mono text-xs lg:inline">
              {{ hit.id.slice(0, 12) }}…
            </code>
          </button>
        </div>

        <template v-if="threadTask">
          <div class="flex items-center gap-2 border-t pt-3">
            <TaskStatusBadge :status="threadTask.status" />
            <span class="text-sm font-medium">{{ threadTask.title }}</span>
            <code class="text-muted-foreground ml-auto hidden font-mono text-xs md:inline">
              {{ threadTask.id }}
            </code>
          </div>

          <div v-if="threadLoading && !threadItems.length" class="flex flex-col gap-2">
            <Skeleton v-for="i in 4" :key="i" class="h-14" :style="{ width: `${94 - i * 5}%` }" />
          </div>

          <template v-else-if="threadItems.length">
            <div class="flex flex-col gap-1.5">
              <div
                v-for="msg in threadItems"
                :key="msg.id"
                class="flex flex-col gap-1 rounded-lg border p-3"
                :class="isOwn(msg) ? 'bg-primary/5' : ''"
              >
                <div class="flex flex-wrap items-center gap-2 text-xs">
                  <span class="text-sm font-medium">{{ memberName(msg.sender_id) }}</span>
                  <span v-if="isOwn(msg)" class="text-muted-foreground">（我）</span>
                  <span class="text-muted-foreground ml-auto">{{ fmtTime(msg.created_at) }}</span>
                </div>
                <p class="text-sm break-all whitespace-pre-wrap">{{ msg.body }}</p>
              </div>
            </div>
            <Button
              v-if="threadCursor"
              variant="outline"
              size="sm"
              class="self-start"
              :disabled="threadLoading"
              @click="loadThread({ append: true })"
            >
              加载更新的消息
            </Button>
          </template>

          <Empty v-else-if="threadLoaded" class="border">
            <EmptyHeader>
              <EmptyTitle>该任务还没有消息。</EmptyTitle>
              <EmptyDescription>用下方表单发起第一条任务消息。</EmptyDescription>
            </EmptyHeader>
          </Empty>
        </template>

        <Empty v-else class="border">
          <EmptyHeader>
            <EmptyTitle>选择一个任务查看线程。</EmptyTitle>
            <EmptyDescription>线程按时间正序展示该任务的全部消息。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>

    <!-- 发送表单（两视角共用；进入任务线程时自动对准该任务）-->
    <Card>
      <CardHeader class="gap-1.5">
        <CardTitle class="flex items-center gap-2 text-base">
          <Send class="size-4" />
          发送消息
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex flex-wrap items-end gap-3">
          <div class="w-36">
            <Label class="text-muted-foreground mb-1 block text-xs">目标类型</Label>
            <Select v-model="sendTarget">
              <SelectTrigger class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="workspace">广播（workspace）</SelectItem>
                <SelectItem value="actor">私信（actor）</SelectItem>
                <SelectItem value="task">任务（task）</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div v-if="sendTarget === 'actor'" class="w-56">
            <Label class="text-muted-foreground mb-1 block text-xs">收件人</Label>
            <Select v-model="sendActorId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="选择成员" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="mem in members"
                  :key="mem.actor.id"
                  :value="mem.actor.id"
                >
                  {{ mem.actor.display_name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div v-if="sendTarget === 'task'" class="min-w-56 flex-1">
            <Label class="text-muted-foreground mb-1 block text-xs">
              任务{{ sendTaskTitle ? `（已选：${sendTaskTitle}）` : '（搜索后点选）' }}
            </Label>
            <div class="flex gap-2">
              <Input
                v-model="sendPicker.query.value"
                placeholder="任务标题关键词…"
                :disabled="!!sendTaskId"
                @keydown.enter="sendPicker.search()"
              />
              <Button
                v-if="sendTaskId"
                variant="outline"
                size="sm"
                @click="
                  sendTaskId = '';
                  sendTaskTitle = '';
                "
              >
                重选
              </Button>
              <Button v-else size="sm" :disabled="sendPicker.searching.value" @click="sendPicker.search()">
                {{ sendPicker.searching.value ? '搜索中…' : '搜索' }}
              </Button>
            </div>
            <div
              v-if="sendPicker.hits.value.length"
              class="border-input mt-2 flex flex-col gap-1 rounded-lg border border-dashed p-2"
            >
              <button
                v-for="hit in sendPicker.hits.value"
                :key="hit.id"
                type="button"
                class="hover:bg-muted/60 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm"
                @click="selectSendTask(hit)"
              >
                <TaskStatusBadge :status="hit.status" />
                <span class="min-w-0 flex-1 truncate">{{ hit.title }}</span>
              </button>
            </div>
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <Label for="msg-body">内容</Label>
          <Textarea
            id="msg-body"
            v-model="body"
            class="min-h-20"
            placeholder="1–20000 字符…"
            @keydown.enter.exact.prevent="send"
          />
          <p v-if="body && !bodyValid" class="text-destructive text-xs">
            内容需 trim 后非空且不超过 20000 字符。
          </p>
        </div>

        <div class="flex items-center justify-between gap-2">
          <span class="text-muted-foreground text-xs">
            {{
              sendTarget === 'workspace'
                ? '广播对本 workspace 全体成员可见。'
                : sendTarget === 'actor'
                  ? '私信仅收发双方可见。'
                  : '消息进入目标任务线程，随任务可见。'
            }}
          </span>
          <Button size="sm" :disabled="!bodyValid || sending" @click="send">
            <Send class="size-3.5" />
            {{ sending ? '发送中…' : '发送' }}
          </Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
