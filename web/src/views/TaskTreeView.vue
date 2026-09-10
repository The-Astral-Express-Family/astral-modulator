<script setup lang="ts">
// 任务树视图（v2 容器语义，TODO.md D15）：
// - 逐容器懒加载：根层 = workspace children 集合；展开节点 = 该任务 children
//   集合（只回传直接子层，行内带 tags/children_count）；不再全量平铺拉取；
// - 过滤（status/tag）服务端生效：变更后重载所有可见集合；
// - 搜索模式：task-search 平面查询（regex/fuzzy/tag/status/assignee 至少其一）；
// - SSE 实时刷新：task.* 防抖重载可见集合，snapshot.required 立即全量重拉
//   （游标超窗语义），tag.* 刷新打开中的详情。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { formatApiError } from '../api/client'
import {
  getTask,
  listTaskChildren,
  listWorkspaceChildren,
  searchTasks,
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
const sseState = ref<'connecting' | 'open' | 'closed'>('connecting')

let unsubscribe: (() => void) | null = null
let reloadTimer: ReturnType<typeof setTimeout> | null = null

const memberNames = computed(() => {
  const m = new Map<string, string>()
  for (const mem of members.value) m.set(mem.actor.id, mem.actor.display_name)
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
  const page = await listWorkspaceChildren(workspaceId.value, filterParams())
  return page.items
}

async function fetchChildren(taskId: string): Promise<Task[]> {
  const page = await listTaskChildren(taskId, filterParams())
  return page.items
}

// 重载所有可见集合：根层 + 每个「已展开且有缓存」的容器。集合数量 = 展开的
// 节点数，与树规模解耦（v1 全量平铺的问题就此了结）。
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
    containers.forEach((id, i) => childrenByContainer.value.set(id, freshChildren[i]))
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
    void reloadVisible()
    return
  }
  if (env.type.startsWith('task.')) scheduleReload()
  else if (env.type.startsWith('tag.')) {
    void reloadVisible()
    void refreshDetail()
  }
}

// ---- 树展开 / 扁平化 ----

async function toggle(task: Task): Promise<void> {
  const next = new Set(expanded.value)
  if (next.has(task.id)) {
    next.delete(task.id)
  } else {
    next.add(task.id)
    if (!childrenByContainer.value.has(task.id)) {
      loading.value = true
      try {
        childrenByContainer.value.set(task.id, await fetchChildren(task.id))
        error.value = null
      } catch (e) {
        error.value = formatApiError(e)
        next.delete(task.id)
      } finally {
        loading.value = false
      }
    }
  }
  expanded.value = next
}

interface FlatRow {
  task: Task
  depth: number
}

const rows = computed<FlatRow[]>(() => {
  const out: FlatRow[] = []
  const walk = (list: Task[], depth: number): void => {
    for (const task of list) {
      out.push({ task, depth })
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
  Boolean(regexInput.value || fuzzyInput.value || tagFilter.value || statusFilter.value),
)

async function runSearch(): Promise<void> {
  if (!canSearch.value) {
    error.value = '搜索至少需要一个条件（regex / fuzzy / tag / status）'
    return
  }
  loading.value = true
  try {
    const page = await searchTasks(workspaceId.value, {
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

const hits = ref<TaskSearchHit[]>([])

function resetToTree(): void {
  regexInput.value = ''
  fuzzyInput.value = ''
  assigneeInput.value = ''
  tagFilter.value = ''
  statusFilter.value = ''
  mode.value = 'tree'
  void reloadVisible()
}

async function load(): Promise<void> {
  await session.boot()
  await reloadVisible()
  try {
    const [mem, tags] = await Promise.all([
      listMembers(workspaceId.value),
      listTags(workspaceId.value),
    ])
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
  roots.value = []
  childrenByContainer.value = new Map()
  expanded.value = new Set()
  hits.value = []
  selected.value = null
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
      <input v-model="tagFilter" placeholder="tag" list="tag-options" />
      <datalist id="tag-options">
        <option v-for="t in tagDict" :key="t.id" :value="t.name" />
      </datalist>
      <select v-model="statusFilter">
        <option value="">全部状态</option>
        <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
      </select>
      <input v-model="assigneeInput" placeholder="assignee id（可选）" />
      <button :disabled="!canSearch || loading" @click="runSearch">查询</button>
      <button :disabled="loading" @click="resetToTree">返回树</button>
    </div>
    <p v-if="error" style="color: #b3261e">{{ error }}</p>
  </div>

  <div v-if="mode === 'tree'" class="card">
    <p class="muted">逐容器懒加载：点击展开箭头拉取该任务直接子层。</p>
    <p v-if="loading && !rows.length" class="muted">加载中…</p>
    <p v-else-if="!rows.length" class="muted">没有任务。</p>
    <table v-else class="task-table">
      <thead>
        <tr>
          <th style="text-align: left">标题</th>
          <th style="text-align: left">状态</th>
          <th style="text-align: left">优先级</th>
          <th style="text-align: left">子任务</th>
          <th style="text-align: left">Tags</th>
          <th style="text-align: left">负责人</th>
          <th style="text-align: left">更新时间</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.task.id">
          <td :style="{ paddingLeft: `${row.depth * 24 + 8}px` }">
            <button v-if="row.task.children_count > 0" class="toggle" @click="toggle(row.task)">
              {{ expanded.has(row.task.id) ? '▾' : '▸' }}
            </button>
            <span v-else class="toggle muted">·</span>
            <a href="#" @click.prevent="openDetail(row.task)">{{ row.task.title }}</a>
          </td>
          <td>
            <span class="badge" :class="`badge-${row.task.status}`">{{ row.task.status }}</span>
          </td>
          <td>{{ row.task.priority }}</td>
          <td>{{ row.task.children_count > 0 ? row.task.children_count : '—' }}</td>
          <td>
            <span v-for="t in row.task.tags" :key="t.id" class="badge badge-tag">{{ t.name }}</span>
            <span v-if="!row.task.tags.length" class="muted">—</span>
          </td>
          <td>{{ memberName(row.task.assignee_actor_id) }}</td>
          <td>{{ fmtTime(row.task.updated_at) }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <div v-else class="card">
    <p class="muted">查询结果：{{ hits.length }} 条</p>
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
  </div>

  <div v-if="selected" class="card">
    <h3>
      {{ selected.title }}
      <span class="badge" :class="`badge-${selected.status}`">{{ selected.status }}</span>
    </h3>
    <p class="muted">
      <code>{{ selected.id }}</code> · revision {{ selected.revision }} · 优先级
      {{ selected.priority }}
    </p>
    <p>负责人：{{ memberName(selected.assignee_actor_id) }}</p>
    <p v-if="selected.parent_id">父任务：<code>{{ selected.parent_id }}</code></p>
    <p>
      Tags：
      <span v-if="selected.tags.length">
        <span v-for="t in selected.tags" :key="t.id" class="badge badge-tag">{{ t.name }}</span>
      </span>
      <span v-else class="muted">（无）</span>
    </p>
    <p v-if="selected.lease">
      租约：{{ memberName(selected.lease.holder_actor_id) }} 至
      {{ fmtTime(selected.lease.expires_at) }}
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
.badge-tag { background: #e0f2f1; color: #00695c; }
</style>
