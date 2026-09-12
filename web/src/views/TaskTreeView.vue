<script setup lang="ts">
// 任务树视图（v2 容器语义，TODO.md D15；round 25 起为双栏设计）：
// - 逐容器懒加载：根层 = workspace children 集合；展开节点 = 该任务 children
//   集合（只回传直接子层，行内带 tags/children_count）；不再全量平铺拉取；
// - 过滤（status/tag）服务端生效：变更后重载所有可见集合；
// - 搜索模式：task-search 平面查询（regex/fuzzy/tag/status/assignee 至少其一）；
// - SSE 实时刷新：task.* 防抖重载可见集合，snapshot.required 立即全量重拉
//   （游标超窗语义），tag.* 刷新打开中的详情；
// - 双栏：左树 + 右详情面板（编辑/标签/认领租约/建子任务，round 25 自任务树
//   round13 分支移植）；数据源经 @/api/taskSource（mock/真实可切换）。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ListTree, Plus } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { formatApiError } from '../api/client'
import { taskApi } from '../api/taskSource'
import type { TaskCreatePayload } from '../api/modules/task'
import type { EventEnvelope, Member, Tag, Task, TaskSearchHit, TaskStatus } from '../api/types'
import { TASKS_MOCK } from '../lib/mockMode'
import { useTaskLiveEvents } from '../composables/useTaskLiveEvents'
import type { SseState } from '../composables/useEventStream'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import EventSimulator from '@/components/tasks/EventSimulator.vue'
import TaskCreateDialog from '@/components/tasks/TaskCreateDialog.vue'
import TaskDetailPanel from '@/components/tasks/TaskDetailPanel.vue'
import TaskFilters from '@/components/tasks/TaskFilters.vue'
import TaskPriorityIcon from '@/components/tasks/TaskPriorityIcon.vue'
import TaskStatusBadge from '@/components/tasks/TaskStatusBadge.vue'
import TaskTreeRow from '@/components/tasks/TaskTreeRow.vue'

const route = useRoute()
// 路由参数保持响应式：组件复用（/workspaces/a/tasks → /workspaces/b/tasks）时正确重载。
const workspaceId = computed(() => route.params.workspaceId as string)

const PAGE_LIMIT = 200

const roots = ref<Task[]>([])
// 已加载的 task 容器集合（直接子层），按容器 id 缓存。
const childrenByContainer = ref<Map<string, Task[]>>(new Map())
const expanded = ref<Set<string>>(new Set())

const mode = ref<'tree' | 'search'>('tree')
const statusFilter = ref<TaskStatus | ''>('')
const tagFilter = ref('')
const regexInput = ref('')
const fuzzyInput = ref('')
const assigneeInput = ref('')

const members = ref<Member[]>([])
const tagDict = ref<Tag[]>([])
const selected = ref<Task | null>(null)
const error = ref<string | null>(null)
const loading = ref(false)
const hits = ref<TaskSearchHit[]>([])

// 加载态细化：展开容器的子层骨架 / 详情面板骨架 / 行更新闪烁
const expandingIds = ref<Set<string>>(new Set())
const detailLoading = ref(false)
const flashIds = ref<Set<string>>(new Set())
const seenRevisions = ref(new Map<string, number>())
let flashTimer: ReturnType<typeof setTimeout> | null = null

const createOpen = ref(false)
const createParent = ref<{ id: string | null; title: string | null }>({ id: null, title: null })
const simulatorOpen = ref(false)

const { state: sseState } = useTaskLiveEvents(workspaceId, onEvent)
let reloadTimer: ReturnType<typeof setTimeout> | null = null

const memberNames = computed(() => {
  const m = new Map<string, string>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor.display_name)
  return m
})
const membersById = computed(() => {
  const m = new Map<string, Member['actor']>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor)
  return m
})
function memberName(id: string | null): string {
  if (!id) return '—'
  return memberNames.value.get(id) ?? id
}

function filterParams(): { status?: string; tag?: string; limit: number } {
  const params: { status?: string; tag?: string; limit: number } = { limit: PAGE_LIMIT }
  if (statusFilter.value) params.status = statusFilter.value
  if (tagFilter.value) params.tag = tagFilter.value
  return params
}

async function fetchRoots(): Promise<Task[]> {
  const page = await taskApi.listWorkspaceChildren(workspaceId.value, filterParams())
  return page.items
}

async function fetchChildren(taskId: string): Promise<Task[]> {
  const page = await taskApi.listTaskChildren(taskId, filterParams())
  return page.items
}

// 重载所有可见集合：根层 + 每个「已展开且有缓存」的容器。集合数量只随展开的
// 节点数增长，与树规模解耦。
async function reloadVisible(): Promise<void> {
  loading.value = true
  try {
    const containers = [...childrenByContainer.value.keys()].filter((id) =>
      expanded.value.has(id),
    )
    const [freshRoots, ...freshChildren] = await Promise.all([
      fetchRoots(),
      ...containers.map((id) => fetchChildren(id)),
    ])
    roots.value = freshRoots
    containers.forEach((id, i) => childrenByContainer.value.set(id, freshChildren[i]!))
    error.value = null
    if (selected.value) await refreshDetail()
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    loading.value = false
  }
}

// task.* 事件高频场景（lease 清扫器批量过期等）防抖合并为一次重载。
function scheduleReload(): void {
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => {
    reloadTimer = null
    void reloadVisible()
  }, 300)
}

async function refreshDetail(): Promise<void> {
  if (!selected.value) return
  try {
    selected.value = await taskApi.getTask(selected.value.id)
  } catch {
    selected.value = null // 被删除/无权限：收起详情
  }
}

async function openDetail(task: Pick<Task, 'id'>): Promise<void> {
  detailLoading.value = true
  try {
    selected.value = await taskApi.getTask(task.id)
    error.value = null
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    detailLoading.value = false
  }
}

function onEvent(env: EventEnvelope): void {
  if (env.type === 'snapshot.required') {
    // 游标超窗断流：SSE 封装会自动重连，这里负责补齐错过的数据。
    toast.warning('事件游标已超出服务端保留窗口，正在重载任务快照…')
    void reloadVisible()
    return
  }
  if (env.type.startsWith('task.')) scheduleReload()
  else if (env.type.startsWith('tag.')) void reloadVisible() // reloadVisible 内部已刷新打开中的详情
}

// ---- 树展开 / 扁平化 ----

async function toggle(task: Task): Promise<void> {
  const next = new Set(expanded.value)
  if (next.has(task.id)) {
    next.delete(task.id)
  } else {
    next.add(task.id)
    if (!childrenByContainer.value.has(task.id)) {
      expandingIds.value = new Set([...expandingIds.value, task.id])
      loading.value = true
      try {
        childrenByContainer.value.set(task.id, await fetchChildren(task.id))
        error.value = null
      } catch (e) {
        error.value = formatApiError(e)
        next.delete(task.id)
      } finally {
        loading.value = false
        const pending = new Set(expandingIds.value)
        pending.delete(task.id)
        expandingIds.value = pending
      }
    }
  }
  expanded.value = next
}

interface FlatRow {
  task: Task
  depth: number
  hasChildren: boolean
  expanded: boolean
}

const rows = computed<FlatRow[]>(() => {
  const out: FlatRow[] = []
  const walk = (list: Task[], depth: number): void => {
    for (const task of list) {
      out.push({ task, depth, hasChildren: task.children_count > 0, expanded: expanded.value.has(task.id) })
      if (expanded.value.has(task.id)) {
        const kids = childrenByContainer.value.get(task.id)
        if (kids) walk(kids, depth + 1)
      }
    }
  }
  walk(roots.value, 0)
  return out
})

// ---- 搜索模式（task-search 平面查询）----

const canSearch = computed(() =>
  Boolean(
    regexInput.value ||
      fuzzyInput.value ||
      tagFilter.value ||
      statusFilter.value ||
      assigneeInput.value,
  ),
)

async function runSearch(): Promise<void> {
  if (!canSearch.value) {
    error.value = '搜索至少需要一个条件（regex / fuzzy / tag / status / assignee）'
    return
  }
  loading.value = true
  try {
    const page = await taskApi.searchTasks(workspaceId.value, {
      regex: regexInput.value || undefined,
      fuzzy: fuzzyInput.value || undefined,
      tag: tagFilter.value || undefined,
      status: statusFilter.value || undefined,
      assignee: assigneeInput.value || undefined,
      limit: 50,
    })
    hits.value = page.items
    mode.value = 'search'
    error.value = null
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    loading.value = false
  }
}

function resetToTree(): void {
  regexInput.value = ''
  fuzzyInput.value = ''
  assigneeInput.value = ''
  tagFilter.value = ''
  statusFilter.value = ''
  mode.value = 'tree'
  void reloadVisible()
}

// 状态/标签过滤在树模式下即时生效（服务端过滤，重载可见集合）。
watch([statusFilter, tagFilter], () => {
  if (mode.value === 'tree') void reloadVisible()
})

async function load(): Promise<void> {
  await reloadVisible()
  try {
    const [mem, tags] = await Promise.all([
      taskApi.listMembers(workspaceId.value),
      taskApi.listTags(workspaceId.value),
    ])
    members.value = mem.items
    tagDict.value = tags.items
  } catch {
    /* 成员/tag 字典失败不阻塞主视图（显示原始 id / 无联想） */
  }
}

// 展开容器；无缓存时立即拉取子层（新建子任务后保证新行可见）。
async function expandContainer(taskId: string): Promise<void> {
  expanded.value = new Set([...expanded.value, taskId])
  if (!childrenByContainer.value.has(taskId)) {
    childrenByContainer.value.set(taskId, await fetchChildren(taskId))
  }
}

// ---- 创建 / 详情写操作回写 ----

function openCreate(parent: Task | null): void {
  createParent.value = parent ? { id: parent.id, title: parent.title } : { id: null, title: null }
  createOpen.value = true
}

async function handleCreate(payload: TaskCreatePayload): Promise<void> {
  createOpen.value = false
  try {
    const created = createParent.value.id
      ? await taskApi.createChild(createParent.value.id, payload)
      : await taskApi.createRoot(workspaceId.value, payload)
    toast.success('任务已创建。')
    if (createParent.value.id) await expandContainer(createParent.value.id)
    void openDetail(created)
    scheduleReload()
  } catch (e) {
    error.value = formatApiError(e)
  }
}

// 详情面板写操作成功：即时回写选中详情 + 防抖刷新行。
function onChanged(task: Task): void {
  if (selected.value?.id === task.id) selected.value = task
  scheduleReload()
}

// 冲突/租约过期：回源刷新选中详情（面板不发第二次写）。
function onStale(): void {
  void refreshDetail()
}

// ---- 行更新闪烁：revision 变化的可见行走一次底色动画（SSE/模拟事件可感知）----

watch(rows, (next) => {
  let flashed = false
  for (const row of next) {
    const prev = seenRevisions.value.get(row.task.id)
    if (prev !== undefined && prev !== row.task.revision) {
      flashIds.value = new Set([...flashIds.value, row.task.id])
      flashed = true
    }
    seenRevisions.value.set(row.task.id, row.task.revision)
  }
  if (flashed) {
    if (flashTimer) clearTimeout(flashTimer)
    flashTimer = setTimeout(() => {
      flashIds.value = new Set()
    }, 1200)
  }
})

onMounted(load)
watch(workspaceId, () => {
  roots.value = []
  childrenByContainer.value = new Map()
  expanded.value = new Set()
  hits.value = []
  selected.value = null
  mode.value = 'tree'
  seenRevisions.value = new Map()
  flashIds.value = new Set()
  void load()
})
onUnmounted(() => {
  if (reloadTimer) clearTimeout(reloadTimer)
  if (flashTimer) clearTimeout(flashTimer)
})

const SSE_VARIANTS: Record<SseState, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader title="任务树" description="逐容器懒加载 · 实时事件驱动 · 详情面板支持认领租约与标签。" />
      <div class="flex items-center gap-2">
        <Badge :variant="SSE_VARIANTS[sseState]">SSE {{ sseState }}</Badge>
        <Button v-if="TASKS_MOCK" variant="outline" size="sm" @click="simulatorOpen = true">
          事件模拟器
        </Button>
        <Button size="sm" @click="openCreate(null)">
          <Plus class="size-4" />
          新建任务
        </Button>
      </div>
    </div>

    <p
      v-if="TASKS_MOCK"
      class="text-muted-foreground rounded-lg border border-dashed px-3 py-2 text-xs"
    >
      演示数据模式（VITE_TASKS_MOCK=1）：数据为内存夹具、事件由模拟器注入，不依赖后端；
      置 0 切换真实 API。
    </p>

    <TaskFilters
      v-model:fuzzy="fuzzyInput"
      v-model:regex="regexInput"
      v-model:assignee="assigneeInput"
      v-model:status="statusFilter"
      v-model:tag="tagFilter"
      :tags="tagDict"
      @search="runSearch"
      @reset="resetToTree"
    />

    <p v-if="error" class="text-destructive text-sm">{{ error }}</p>

    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_400px]">
      <!-- 左栏：树 / 搜索结果 -->
      <Card class="flex min-h-[60vh] flex-col overflow-hidden">
        <CardContent class="min-h-0 flex-1 overflow-y-auto p-2">
          <!-- 初始加载骨架（整树） -->
          <div v-if="loading && !rows.length && mode === 'tree'" class="flex flex-col gap-2 p-2">
            <Skeleton v-for="i in 8" :key="i" class="h-8" :style="{ width: `${95 - i * 6}%` }" />
          </div>

          <!-- 搜索骨架 -->
          <div v-else-if="loading && mode === 'search' && !hits.length" class="flex flex-col gap-2 p-2">
            <Skeleton v-for="i in 6" :key="i" class="h-9 w-full" />
          </div>

          <!-- 搜索结果模式 -->
          <template v-else-if="mode === 'search'">
            <p class="text-muted-foreground px-2 py-1 text-xs">
              查询结果（{{ hits.length }} 条）——「返回树」恢复树模式。
            </p>
            <TransitionGroup v-if="hits.length" tag="div" name="tree-row" class="relative flex flex-col">
              <button
                v-for="hit in hits"
                :key="hit.id"
                type="button"
                class="hover:bg-muted/60 flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left"
                :class="[
                  selected?.id === hit.id ? 'bg-muted' : '',
                  flashIds.has(hit.id) ? 'task-row-flash' : '',
                ]"
                @click="openDetail(hit)"
              >
                <TaskStatusBadge :status="hit.status" show-label class="w-[62px] shrink-0" />
                <TaskPriorityIcon :priority="hit.priority" />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm">{{ hit.title }}</span>
                  <span class="text-muted-foreground block truncate text-xs">
                    {{ memberName(hit.assignee_actor_id) }}
                  </span>
                </span>
                <Badge
                  v-for="t in hit.tags.slice(0, 2)"
                  :key="t.id"
                  variant="outline"
                  class="hidden shrink-0 lg:inline-flex"
                >
                  {{ t.name }}
                </Badge>
                <Badge v-if="hit.score !== undefined" variant="outline" class="shrink-0">
                  匹配 {{ Math.round(hit.score * 100) }}%
                </Badge>
              </button>
            </TransitionGroup>
            <Empty v-if="!loading && !hits.length">
              <EmptyHeader>
                <EmptyTitle>没有匹配的任务。</EmptyTitle>
                <EmptyDescription>放宽过滤条件，或换一个关键词。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </template>

          <!-- 树模式 -->
          <template v-else>
            <TransitionGroup v-if="rows.length" tag="div" name="tree-row" class="relative flex flex-col">
              <template v-for="row in rows" :key="row.task.id">
                <TaskTreeRow
                  :row="row"
                  :selected="selected?.id === row.task.id"
                  :assignee="membersById.get(row.task.assignee_actor_id ?? '') ?? null"
                  :flash="flashIds.has(row.task.id)"
                  @select="openDetail(row.task)"
                  @toggle="toggle(row.task)"
                />
                <!-- 展开容器的子层懒加载骨架 -->
                <div
                  v-if="expandingIds.has(row.task.id)"
                  :key="`${row.task.id}:skeleton`"
                  class="mb-1 flex flex-col gap-1 py-1"
                  :style="{ paddingLeft: `${(row.depth + 1) * 18 + 6}px` }"
                >
                  <Skeleton class="h-7 w-1/2" />
                  <Skeleton class="h-7 w-1/3" />
                </div>
              </template>
            </TransitionGroup>
            <Empty v-if="!loading && !rows.length && !error">
              <EmptyHeader>
                <EmptyTitle>没有任务。</EmptyTitle>
                <EmptyDescription>当前过滤条件下无结果，或工作区还没有任务。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </template>
        </CardContent>
        <div class="text-muted-foreground border-t px-3 py-1.5 text-xs">
          {{
            mode === 'search'
              ? `搜索 ${hits.length} 条`
              : `${rows.length} 行可见（展开 ${expanded.size} 个容器）`
          }}
          <template v-if="statusFilter || tagFilter"> · 过滤中</template>
        </div>
      </Card>

      <!-- 右栏：详情面板（切换任务淡入；加载时结构化骨架） -->
      <div class="min-h-[60vh]">
        <Transition name="detail" mode="out-in">
          <Card v-if="detailLoading" key="skeleton" class="flex h-full flex-col">
            <CardHeader class="gap-2">
              <Skeleton class="h-5 w-2/3" />
              <div class="flex gap-2">
                <Skeleton class="h-7 w-28" />
                <Skeleton class="h-7 w-24" />
              </div>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
              <div class="flex flex-col gap-1.5">
                <Skeleton class="h-4 w-12" />
                <Skeleton class="h-16 w-full" />
              </div>
              <Skeleton class="h-px w-full" />
              <div class="flex gap-1.5">
                <Skeleton class="h-5 w-16 rounded-full" />
                <Skeleton class="h-5 w-14 rounded-full" />
              </div>
              <Skeleton class="h-24 w-full rounded-lg" />
              <Skeleton class="h-4 w-2/5" />
            </CardContent>
          </Card>
          <TaskDetailPanel
            v-else-if="selected"
            :key="selected.id"
            :task="selected"
            :actors-by-id="membersById"
            :all-tags="tagDict"
            @changed="onChanged"
            @stale="onStale"
            @create-subtask="openCreate(selected)"
          />
          <Card v-else key="empty" class="flex min-h-[60vh] flex-col">
            <CardContent class="flex flex-1 items-center justify-center">
              <Empty>
                <EmptyHeader>
                  <EmptyTitle class="flex items-center gap-2">
                    <ListTree class="size-4" />
                    选择一个任务
                  </EmptyTitle>
                  <EmptyDescription>
                    左侧选择任务后，在此查看详情、编辑字段、管理标签与认领租约。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </CardContent>
          </Card>
        </Transition>
      </div>
    </div>

    <TaskCreateDialog
      v-model:open="createOpen"
      :tags="tagDict"
      :parent-title="createParent.title"
      @submit="handleCreate"
    />

    <EventSimulator
      v-if="TASKS_MOCK"
      v-model:open="simulatorOpen"
      :selected-task-id="selected?.id ?? null"
      :tags="tagDict"
    />
  </div>
</template>

<style scoped>
/* 树行进出场/重排：展开、收起、过滤变化时平滑过渡。
   leave 用 absolute 让留存行立即上移（配合 v-move 动画）。 */
.tree-row-enter-active {
  transition: opacity 160ms ease, transform 160ms ease;
}
.tree-row-enter-from {
  opacity: 0;
  transform: translateY(-4px);
}
.tree-row-leave-active {
  position: absolute;
  width: 100%;
  transition: opacity 120ms ease;
}
.tree-row-leave-to {
  opacity: 0;
}
.tree-row-move {
  transition: transform 180ms ease;
}

/* 详情面板切换淡入 */
.detail-enter-active,
.detail-leave-active {
  transition: opacity 140ms ease, transform 140ms ease;
}
.detail-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.detail-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .tree-row-enter-active,
  .tree-row-leave-active,
  .tree-row-move,
  .detail-enter-active,
  .detail-leave-active {
    transition: none;
  }
}
</style>
