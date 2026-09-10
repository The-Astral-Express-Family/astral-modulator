<script setup lang="ts">
// 任务树视图（TODO.md §11 第 4 项）：
// - 树模式（默认）：list 端点全量分页拉取 → 前端按 parent_id 组树；
// - 搜索模式：search 端点（regex 过滤 / fuzzy 排序，可叠加 tag/status，
//   服务端语义 architecture §13）；
// - SSE 实时刷新：task.* 事件防抖重载，snapshot.required 立即全量重拉
//   （游标超窗语义，protocol.md §5）；tag.* 刷新打开中的详情（tags 仅详情
//   响应填充，D11）；
// - 详情侧栏：getTask（tags/lease 仅详情响应填充）。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { formatApiError } from '../api/client'
import {
  getTask,
  listTasks,
  searchTasks,
  type TaskListParams,
  type TaskSearchParams,
} from '../api/modules/task'
import { listMembers, listTags } from '../api/modules/workspace'
import { subscribeEvents } from '../api/sse'
import { useSessionStore } from '../stores/session'
import type { EventEnvelope, Member, Tag, Task, TaskSearchHit, TaskStatus } from '../api/types'

const route = useRoute()
const session = useSessionStore()
// 路由参数保持响应式：组件复用（/workspaces/a/tasks → /workspaces/b/tasks）时正确重载。
const workspaceId = computed(() => route.params.workspaceId as string)

const STATUSES: TaskStatus[] = ['open', 'in_progress', 'blocked', 'review', 'done', 'cancelled']

interface TaskNode {
  task: Task
  children: TaskNode[]
  /** parent_id 指向不存在的任务（防御：服务端已有 parent 校验，正常不应出现） */
  orphan: boolean
}

const tasks = ref<Task[]>([])
const hits = ref<TaskSearchHit[]>([])
const searchNext = ref<string | null>(null)
const mode = ref<'tree' | 'search'>('tree')
const statusFilter = ref<TaskStatus | ''>('')
const regexInput = ref('')
const fuzzyInput = ref('')
const tagInput = ref('')

const members = ref<Member[]>([])
const tagDict = ref<Tag[]>([])
const selected = ref<Task | null>(null)
const collapsed = ref<Set<string>>(new Set())
const error = ref<string | null>(null)
const loading = ref(false)
const sseState = ref<'connecting' | 'open' | 'closed'>('connecting')

let unsubscribe: (() => void) | null = null
let reloadTimer: ReturnType<typeof setTimeout> | null = null
let inFlight = 0

const memberNames = computed(() => {
  const m = new Map<string, string>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor.display_name)
  return m
})
function memberName(id: string | null): string {
  if (!id) return '—'
  return memberNames.value.get(id) ?? id
}

async function fetchAllTasks(): Promise<Task[]> {
  const out: Task[] = []
  let cursor: string | undefined
  do {
    const params: TaskListParams = { limit: 200 }
    if (cursor) params.cursor = cursor
    const page = await listTasks(workspaceId.value, params)
    out.push(...page.items)
    cursor = page.next_cursor ?? undefined
  } while (cursor)
  return out
}

// 组树：parent 在集合内 → 挂为子节点；否则（含孤儿）作根。兄弟按创建序（id
// 为 UUIDv7，字典序即时间序）。
const tree = computed<TaskNode[]>(() => {
  const byId = new Map<string, TaskNode>()
  for (const t of tasks.value) byId.set(t.id, { task: t, children: [], orphan: false })
  const roots: TaskNode[] = []
  for (const node of byId.values()) {
    const pid = node.task.parent_id
    const parent = pid ? byId.get(pid) : undefined
    if (parent) parent.children.push(node)
    else {
      node.orphan = Boolean(pid)
      roots.push(node)
    }
  }
  const sortRec = (nodes: TaskNode[]): void => {
    nodes.sort((a, b) => (a.task.id < b.task.id ? -1 : a.task.id > b.task.id ? 1 : 0))
    for (const n of nodes) sortRec(n.children)
  }
  sortRec(roots)
  if (!statusFilter.value) return roots
  // 状态过滤保留「命中节点 + 其命中后代路径」；此时不再受折叠状态约束。
  const filterRec = (nodes: TaskNode[]): TaskNode[] => {
    const kept: TaskNode[] = []
    for (const n of nodes) {
      const children = filterRec(n.children)
      if (n.task.status === statusFilter.value || children.length) {
        kept.push({ ...n, children })
      }
    }
    return kept
  }
  return filterRec(roots)
})

// 扁平化为可见行（避免递归组件）；折叠集合仅在无状态过滤时生效。
interface FlatRow {
  node: TaskNode
  depth: number
}
const rows = computed<FlatRow[]>(() => {
  const out: FlatRow[] = []
  const filtering = Boolean(statusFilter.value)
  const walk = (nodes: TaskNode[], depth: number): void => {
    for (const n of nodes) {
      out.push({ node: n, depth })
      if (filtering || !collapsed.value.has(n.task.id)) walk(n.children, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})
function hasChildren(node: TaskNode): boolean {
  return node.children.length > 0
}
function toggle(node: TaskNode): void {
  const id = node.task.id
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

// ---- 搜索模式 ----
const canSearch = computed(() => Boolean(regexInput.value || fuzzyInput.value))

async function runSearch(append = false): Promise<void> {
  const params: TaskSearchParams = { limit: 50 }
  if (regexInput.value) params.regex = regexInput.value
  if (fuzzyInput.value) params.fuzzy = fuzzyInput.value
  if (tagInput.value) params.tag = tagInput.value
  if (statusFilter.value) params.status = statusFilter.value
  if (append && searchNext.value) params.cursor = searchNext.value
  const page = await searchTasks(workspaceId.value, params)
  hits.value = append ? [...hits.value, ...page.items] : page.items
  searchNext.value = page.next_cursor
  mode.value = 'search'
}

function resetToTree(): void {
  regexInput.value = ''
  fuzzyInput.value = ''
  tagInput.value = ''
  statusFilter.value = ''
  mode.value = 'tree'
  void reload()
}

// ---- 加载 / 刷新 ----
async function reload(): Promise<void> {
  inFlight += 1
  loading.value = true
  try {
    if (mode.value === 'tree') {
      tasks.value = await fetchAllTasks()
    } else {
      await runSearch(false)
    }
    error.value = null
    if (selected.value) await refreshDetail()
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    inFlight -= 1
    if (inFlight === 0) loading.value = false
  }
}

// task.* 事件高频场景（lease 清扫器批量过期等）防抖合并为一次重载。
function scheduleReload(): void {
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => {
    reloadTimer = null
    void reload()
  }, 300)
}

async function refreshDetail(): Promise<void> {
  if (!selected.value) return
  try {
    selected.value = await getTask(selected.value.id)
  } catch {
    selected.value = null // 被删除/无权限：收起详情
  }
}

async function openDetail(task: Pick<Task, 'id'>): Promise<void> {
  try {
    selected.value = await getTask(task.id)
    error.value = null
  } catch (e) {
    error.value = formatApiError(e)
  }
}

function onEvent(env: EventEnvelope): void {
  if (env.type === 'snapshot.required') {
    // 游标超窗断流：SSE 封装会自动重连，这里负责补齐错过的数据。
    void reload()
    return
  }
  if (env.type.startsWith('task.')) scheduleReload()
  else if (env.type.startsWith('tag.')) void refreshDetail()
}

async function load(): Promise<void> {
  await session.boot()
  await reload()
  try {
    const [mem, tags] = await Promise.all([listMembers(workspaceId.value), listTags(workspaceId.value)])
    members.value = mem.items
    tagDict.value = tags.items
  } catch {
    /* 成员/tag 字典失败不阻塞主视图（显示原始 id / 无联想） */
  }
  unsubscribe?.()
  unsubscribe = subscribeEvents({
    workspaceId: workspaceId.value,
    onEvent,
    onStateChange: (s) => (sseState.value = s),
  })
}

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

onMounted(load)
watch(workspaceId, () => {
  tasks.value = []
  hits.value = []
  selected.value = null
  collapsed.value = new Set()
  void load()
})
onUnmounted(() => {
  unsubscribe?.()
  if (reloadTimer) clearTimeout(reloadTimer)
})
</script>

<template>
  <h2>任务树</h2>

  <div class="card">
    <p class="muted">
      workspace: <code>{{ workspaceId }}</code> · SSE：{{ sseState }}
      · <RouterLink :to="`/workspaces/${workspaceId}`">返回总览</RouterLink>
    </p>
    <div class="toolbar">
      <input v-model="regexInput" placeholder="regex（过滤 title/description）" />
      <input v-model="fuzzyInput" placeholder="fuzzy（排序）" />
      <input v-model="tagInput" placeholder="tag" list="tag-options" />
      <datalist id="tag-options">
        <option v-for="t in tagDict" :key="t.id" :value="t.name" />
      </datalist>
      <select v-model="statusFilter">
        <option value="">全部状态</option>
        <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
      </select>
      <button :disabled="!canSearch || loading" @click="runSearch()">搜索</button>
      <button :disabled="loading" @click="resetToTree">重置为树</button>
    </div>
    <p v-if="error" style="color: #b3261e">{{ error }}</p>
  </div>

  <div v-if="mode === 'tree'" class="card">
    <p v-if="loading && !rows.length" class="muted">加载中…</p>
    <p v-else-if="!rows.length" class="muted">没有任务。</p>
    <table v-else class="task-table">
      <thead>
        <tr>
          <th style="text-align: left">标题</th>
          <th style="text-align: left">状态</th>
          <th style="text-align: left">优先级</th>
          <th style="text-align: left">负责人</th>
          <th style="text-align: left">更新时间</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.node.task.id">
          <td :style="{ paddingLeft: `${row.depth * 24 + 8}px` }">
            <button
              v-if="hasChildren(row.node)"
              class="toggle"
              @click="toggle(row.node)"
            >
              {{ collapsed.has(row.node.task.id) ? '▸' : '▾' }}
            </button>
            <span v-else class="toggle muted">·</span>
            <a href="#" @click.prevent="openDetail(row.node.task)">{{ row.node.task.title }}</a>
            <span v-if="row.node.orphan" class="badge badge-warn" title="父任务不存在">孤儿</span>
          </td>
          <td><span class="badge" :class="`badge-${row.node.task.status}`">{{ row.node.task.status }}</span></td>
          <td>{{ row.node.task.priority }}</td>
          <td>{{ memberName(row.node.task.assignee_actor_id) }}</td>
          <td>{{ fmtTime(row.node.task.updated_at) }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <div v-else class="card">
    <p class="muted">搜索结果：{{ hits.length }} 条{{ searchNext ? '（还有更多）' : '' }}</p>
    <p v-if="!hits.length" class="muted">没有匹配的任务。</p>
    <table v-if="hits.length" class="task-table">
      <thead>
        <tr>
          <th style="text-align: left">标题</th>
          <th style="text-align: left">状态</th>
          <th style="text-align: left">优先级</th>
          <th style="text-align: left">负责人</th>
          <th style="text-align: left">score</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="t in hits" :key="t.id">
          <td><a href="#" @click.prevent="openDetail(t)">{{ t.title }}</a></td>
          <td><span class="badge" :class="`badge-${t.status}`">{{ t.status }}</span></td>
          <td>{{ t.priority }}</td>
          <td>{{ memberName(t.assignee_actor_id) }}</td>
          <td>{{ t.score !== undefined ? t.score.toFixed(3) : '—' }}</td>
        </tr>
      </tbody>
    </table>
    <button v-if="searchNext" :disabled="loading" @click="runSearch(true)">加载更多</button>
  </div>

  <div v-if="selected" class="card">
    <h3>
      {{ selected.title }}
      <span class="badge" :class="`badge-${selected.status}`">{{ selected.status }}</span>
    </h3>
    <p class="muted"><code>{{ selected.id }}</code> · revision {{ selected.revision }} · 优先级 {{ selected.priority }}</p>
    <p>负责人：{{ memberName(selected.assignee_actor_id) }}</p>
    <p v-if="selected.parent_id">父任务：<code>{{ selected.parent_id }}</code></p>
    <p>
      Tags：
      <span v-if="selected.tags?.length">
        <span v-for="t in selected.tags" :key="t.id" class="badge badge-tag">{{ t.name }}</span>
      </span>
      <span v-else class="muted">（无）</span>
    </p>
    <p v-if="selected.lease">
      租约：{{ memberName(selected.lease.holder_actor_id) }} 至 {{ fmtTime(selected.lease.expires_at) }}
    </p>
    <p v-else class="muted">无租约。</p>
    <pre v-if="selected.description">{{ selected.description }}</pre>
    <p class="muted">创建 {{ fmtTime(selected.created_at) }} · 更新 {{ fmtTime(selected.updated_at) }}</p>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.toolbar input,
.toolbar select {
  padding: 6px 8px;
  border: 1px solid #d0d3d8;
  border-radius: 6px;
}
.toolbar input[list] {
  width: 90px;
}
.task-table td,
.task-table th {
  padding: 6px 8px;
  border-bottom: 1px solid #eceef1;
}
.toggle {
  width: 20px;
  border: none;
  background: none;
  cursor: pointer;
  padding: 0;
}
.badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  background: #eceef1;
  font-size: 12px;
  margin-right: 4px;
}
.badge-open { background: #e3edfd; color: #1a56b8; }
.badge-in_progress { background: #fff3d6; color: #8a6100; }
.badge-blocked { background: #fde3e3; color: #b3261e; }
.badge-review { background: #ede1fb; color: #6b21a8; }
.badge-done { background: #ddf3e2; color: #1b7f3b; }
.badge-cancelled { background: #eceef1; color: #6b7075; }
.badge-warn { background: #fde3e3; color: #b3261e; }
.badge-tag { background: #e0f2f1; color: #00695c; }
</style>
