// 任务数据源的内存实现（mock 模式，v2 容器语义）：复刻服务端已裁决的语义，
// 供设计演示与离线开发。对齐点：集合（workspace/task children）按兄弟排序键
// position 升序（同位 id 降序兜底，2.2）+ status/tag/assignee 过滤；
// task-search 需 ≥1 条件（fuzzy 打分 0.85*title + 0.15*description）；
// 创建 = 向容器 POST children（tags 按名解析，缺失 → 404，children_count 同步，
// position 追加兄弟尾部）；move = 换父 + 兄弟内重排（摘除-让位-落位，2.2）；
// claim 原子抢租约；release 清租约+清 assignee+in_progress→open；attach/detach
// 幂等（已关联不 bump）；租约过期清扫（assignee 清空、in_progress→open、
// revision+1、task.lease.expired）。
// 所有 API 注入 ~250ms 延迟，让骨架屏/busy 态在 mock 下真实可见。
// 错误以 `CODE: 文案` 形式的 Error 抛出（async 函数内 throw → rejected Promise）。

import { useSessionStore } from '@/stores/session'
import type { TaskApi } from '@/api/taskSource'
import type {
  TaskCreatePayload,
  TaskFilterParams,
  TaskMoveItem,
  TaskSearchParams,
  TaskUpdatePayload,
} from '@/api/modules/task'
import type {
  EventEnvelope,
  Lease,
  Member,
  Page,
  Task,
  TaskSearchHit,
  Tag,
} from '@/api/types'
import {
  DEMO_MEMBERS,
  DEMO_TASKS,
  DEMO_TAGS,
  DEMO_WORKSPACE_ID,
  OTHER_ACTORS,
  fakeId,
} from './fixture'

const clone = <T>(v: T): T => structuredClone(v)

// 注入网络延迟：让骨架屏/busy 态在 mock 模式下真实可见（约等于本地 API 往返）。
const MOCK_LATENCY_MS = 250
const sleep = (ms: number = MOCK_LATENCY_MS): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms))

function err(code: string, message: string): never {
  throw new Error(`${code}: ${message}`)
}

// ---- 事件总线（模拟 SSE）----

type Listener = (env: EventEnvelope) => void
const listeners = new Set<Listener>()
let sweepTimer: ReturnType<typeof setInterval> | null = null

function emitEvent(type: string, data: Record<string, unknown>, resourceRevision?: number): void {
  const env: EventEnvelope = {
    id: `evt_${crypto.randomUUID()}`,
    type,
    workspace_id: DEMO_WORKSPACE_ID,
    occurred_at: new Date().toISOString(),
    schema_version: 1,
    ...(resourceRevision !== undefined ? { resource_revision: resourceRevision } : {}),
    data,
  }
  for (const listener of listeners) listener(env)
}

export function subscribeMockEvents(opts: {
  workspaceId: string
  onEvent: Listener
  onStateChange?: (state: 'connecting' | 'open' | 'closed') => void
}): () => void {
  listeners.add(opts.onEvent)
  opts.onStateChange?.('open') // mock 流即时「连接成功」
  if (sweepTimer === null) {
    // 租约过期清扫：夹具里 5 分钟后过期的租约到点自动触发 task.lease.expired 演示。
    sweepTimer = setInterval(sweepExpiredLeases, 3_000)
  }
  return () => {
    listeners.delete(opts.onEvent)
    if (listeners.size === 0 && sweepTimer !== null) {
      clearInterval(sweepTimer)
      sweepTimer = null
    }
  }
}

// ---- 内存存储 ----

const tasks = new Map<string, Task>(DEMO_TASKS.map((t) => [t.id, clone(t)]))
const tags = clone(DEMO_TAGS)

function selfActorId(): string {
  const session = useSessionStore()
  return session.actor?.id ?? ''
}

function taskOrThrow(taskId: string): Task {
  const t = tasks.get(taskId)
  if (!t) err('TASK_NOT_FOUND', '任务不存在或不可见。')
  return t
}

function checkRevision(t: Task, expected: number): void {
  if (t.revision !== expected) {
    err('REVISION_CONFLICT', `任务已被他人修改（当前 revision ${t.revision}），请刷新后重试。`)
  }
}

function leaseActive(t: Task): boolean {
  return t.lease !== null && t.lease !== undefined && new Date(t.lease.expires_at).getTime() > Date.now()
}

function sweepExpiredLeases(): void {
  const now = Date.now()
  for (const t of tasks.values()) {
    if (!t.lease) continue
    if (new Date(t.lease.expires_at).getTime() >= now) continue // 未过期，跳过
    const holder = t.lease.holder_actor_id
    applyLeaseLoss(t, holder)
  }
}

// 服务端语义：清租约 + 清 assignee + in_progress→open + revision+1 + task.lease.expired。
function applyLeaseLoss(t: Task, previousHolder: string): void {
  t.lease = null
  t.assignee_actor_id = null
  if (t.status === 'in_progress') t.status = 'open'
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.lease.expired', { task_id: t.id, previous_holder: previousHolder }, t.revision)
}

function applyFilters(items: Task[], params: TaskFilterParams): Task[] {
  let out = items
  if (params.status) out = out.filter((t) => t.status === params.status)
  if (params.tag) {
    const wanted = params.tag.toLowerCase()
    out = out.filter((t) => t.tags.some((tg) => tg.name.toLowerCase() === wanted))
  }
  if (params.assignee) out = out.filter((t) => t.assignee_actor_id === params.assignee)
  // 排序口径与服务端一致（2.2）：position 升序、同位 id 降序兜底。
  out.sort((a, b) => a.position - b.position || (a.id < b.id ? 1 : -1))
  if (params.cursor) out = out.filter((t) => t.id < params.cursor!)
  return out
}

// 同一父容器子层的兄弟集合（内存版 siblingScope）。
function siblingsOf(parentId: string | null): Task[] {
  return [...tasks.values()].filter((t) => t.parent_id === parentId)
}

async function listWorkspaceChildren(
  _workspaceId: string,
  params: TaskFilterParams = {},
): Promise<Page<Task>> {
  await sleep()
  const roots = [...tasks.values()].filter((t) => t.parent_id === null)
  const items = applyFilters(roots, params).slice(0, params.limit ?? 50)
  return { items: items.map(clone), next_cursor: null }
}

async function listTaskChildren(
  taskId: string,
  params: TaskFilterParams = {},
): Promise<Page<Task>> {
  await sleep()
  taskOrThrow(taskId)
  const kids = [...tasks.values()].filter((t) => t.parent_id === taskId)
  const items = applyFilters(kids, params).slice(0, params.limit ?? 50)
  return { items: items.map(clone), next_cursor: null }
}

function fuzzyScore(q: string, title: string, description: string): number {
  const nq = q.toLowerCase()
  const hit = (s: string): number => {
    const ns = s.toLowerCase()
    if (ns.includes(nq)) return 1
    if (nq.length > 1 && ns.split(/\s+/).some((w) => w.startsWith(nq))) return 0.6
    return 0
  }
  return 0.85 * hit(title) + 0.15 * hit(description)
}

async function searchTasks(
  _workspaceId: string,
  params: TaskSearchParams,
): Promise<Page<TaskSearchHit>> {
  await sleep()
  if (!params.regex && !params.fuzzy && !params.tag && !params.status && !params.assignee) {
    err('VALIDATION_FAILED', '至少需要一个过滤条件（regex/fuzzy/tag/status/assignee）。')
  }
  let candidates = [...tasks.values()]
  if (params.tag) {
    const wanted = params.tag.toLowerCase()
    candidates = candidates.filter((t) => t.tags.some((tg) => tg.name.toLowerCase() === wanted))
  }
  if (params.status) candidates = candidates.filter((t) => t.status === params.status)
  if (params.assignee) candidates = candidates.filter((t) => t.assignee_actor_id === params.assignee)
  if (params.parent_id) candidates = candidates.filter((t) => t.parent_id === params.parent_id)
  if (params.regex) {
    let re: RegExp
    try {
      re = new RegExp(params.regex)
    } catch {
      err('VALIDATION_FAILED', 'regex 语法无效。')
    }
    candidates = candidates.filter((t) => re.test(t.title) || re.test(t.description))
  }
  let items: TaskSearchHit[] = candidates.map(clone)
  if (params.fuzzy) {
    items = items
      .map((t) => ({ ...t, score: fuzzyScore(params.fuzzy!, t.title, t.description) }))
      .sort((a, b) => (b.score ?? 0) - (a.score ?? 0) || (a.id < b.id ? 1 : -1))
  } else {
    items.sort((a, b) => (a.id < b.id ? 1 : -1))
  }
  items = items.slice(0, params.limit ?? 50)
  return { items, next_cursor: null }
}

async function getTask(taskId: string): Promise<Task> {
  await sleep()
  return clone(taskOrThrow(taskId))
}

function createInContainer(parentId: string | null, payload: TaskCreatePayload): Task {
  const title = payload.title.trim()
  if (!title || title.length > 500) err('VALIDATION_FAILED', '标题长度须为 1–500。')
  if (parentId) taskOrThrow(parentId) // 容器必须存在
  const resolvedTags = (payload.tags ?? []).map((name) => {
    const tag = tags.find((cand) => cand.name.toLowerCase() === name.toLowerCase())
    if (!tag) err('NOT_FOUND', `标签 ${name} 不存在。`)
    return tag
  })
  const now = new Date().toISOString()
  const id = fakeId('tsk', Date.now() % 0xffffffff)
  const t: Task = {
    id,
    workspace_id: DEMO_WORKSPACE_ID,
    parent_id: parentId,
    title,
    description: payload.description ?? '',
    status: 'open',
    priority: payload.priority ?? 'normal',
    assignee_actor_id: null,
    revision: 1,
    position: siblingsOf(parentId).length, // 2.2：创建追加兄弟尾部
    tags: resolvedTags.map(clone),
    children_count: 0,
    created_at: now,
    updated_at: now,
  }
  tasks.set(id, t)
  if (parentId) {
    const parent = tasks.get(parentId)!
    parent.children_count += 1
  }
  emitEvent('task.created', { task_id: id, title }, 1)
  return t
}

async function createRoot(_workspaceId: string, payload: TaskCreatePayload): Promise<Task> {
  await sleep()
  return clone(createInContainer(null, payload))
}

async function createChild(taskId: string, payload: TaskCreatePayload): Promise<Task> {
  await sleep()
  return clone(createInContainer(taskId, payload))
}

async function updateTask(taskId: string, payload: TaskUpdatePayload): Promise<Task> {
  await sleep()
  const t = taskOrThrow(taskId)
  checkRevision(t, payload.expected_revision)
  if (payload.title !== undefined) {
    const title = payload.title.trim()
    if (!title || title.length > 500) err('VALIDATION_FAILED', '标题长度须为 1–500。')
    t.title = title
  }
  if (payload.description !== undefined) t.description = payload.description
  if (payload.status !== undefined) t.status = payload.status as Task['status']
  if (payload.priority !== undefined) t.priority = payload.priority as Task['priority']
  if (payload.assignee_actor_id !== undefined) t.assignee_actor_id = payload.assignee_actor_id
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id }, t.revision)
  return clone(t)
}

async function claimTask(
  taskId: string,
  payload: { expected_revision: number; lease_seconds?: number },
): Promise<{ task: Task; lease: Lease }> {
  await sleep()
  const t = taskOrThrow(taskId)
  checkRevision(t, payload.expected_revision)
  if (leaseActive(t)) err('TASK_ALREADY_CLAIMED', '任务已被他人认领且租约未过期。')
  const holder = selfActorId()
  const lease: Lease = {
    holder_actor_id: holder,
    expires_at: new Date(Date.now() + (payload.lease_seconds ?? 300) * 1000).toISOString(),
  }
  t.lease = lease
  t.assignee_actor_id = holder
  t.status = 'in_progress'
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.claimed', { task_id: t.id, lease_expires_at: lease.expires_at }, t.revision)
  return { task: clone(t), lease: clone(lease) }
}

async function renewLease(taskId: string, leaseSeconds?: number): Promise<Lease> {
  await sleep()
  const t = taskOrThrow(taskId)
  if (!leaseActive(t)) err('TASK_LEASE_EXPIRED', '租约已过期，需重新认领。')
  t.lease!.expires_at = new Date(Date.now() + (leaseSeconds ?? 300) * 1000).toISOString()
  t.lease!.renewed_at = new Date().toISOString()
  return clone(t.lease!)
}

async function releaseLease(taskId: string): Promise<void> {
  await sleep()
  const t = taskOrThrow(taskId)
  if (t.lease) {
    if (t.lease.holder_actor_id !== selfActorId()) {
      err('INSUFFICIENT_SCOPE', '仅租约持有者可释放。')
    }
    t.lease = null
    t.assignee_actor_id = null
    if (t.status === 'in_progress') t.status = 'open'
    t.revision += 1
    t.updated_at = new Date().toISOString()
    emitEvent('task.released', { task_id: t.id }, t.revision)
  }
}

async function attachTaskTag(taskId: string, tagId: string, expectedRevision?: number): Promise<Task> {
  await sleep()
  const t = taskOrThrow(taskId)
  const tag = tags.find((cand) => cand.id === tagId)
  if (!tag) err('NOT_FOUND', '标签不存在。')
  if (t.tags.some((cand) => cand.id === tagId)) return clone(t) // 幂等：不 bump
  if (expectedRevision !== undefined) checkRevision(t, expectedRevision)
  t.tags.push(clone(tag))
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id, tag_change: 'attach', tag_id: tagId }, t.revision)
  return clone(t)
}

async function detachTaskTag(taskId: string, tagId: string): Promise<void> {
  await sleep()
  const t = taskOrThrow(taskId)
  const before = t.tags.length
  t.tags = t.tags.filter((cand) => cand.id !== tagId)
  if (t.tags.length === before) return // 幂等：不 bump
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id, tag_change: 'detach', tag_id: tagId }, t.revision)
}

// moveTasks（2.2）：换父 + 兄弟内重排，复刻服务端摘除-让位-落位语义。
// items 按序生效；position 以摘除前的兄弟列表为准（同父且原位在下标之前
// → 摘除后等效下标 -1），越界收敛到末尾；位移兄弟不 bump revision。
function applyMoveItems(items: TaskMoveItem[]): Task[] {
  const moved: Task[] = []
  for (const it of items) {
    const t = taskOrThrow(it.task_id)
    checkRevision(t, it.expected_revision)
    if (it.parent_id) taskOrThrow(it.parent_id)
    if (it.position !== undefined && it.position < 0) {
      err('VALIDATION_FAILED', 'position 须 ≥ 0。')
    }
    const sameParent =
      (it.parent_id === null && t.parent_id === null) || it.parent_id === t.parent_id
    // 摘除：老列表补洞。
    const oldPos = t.position
    const oldParent = t.parent_id
    for (const sib of siblingsOf(t.parent_id)) {
      if (sib.id !== t.id && sib.position > oldPos) sib.position -= 1
    }
    // 目标下标：缺省 = 末尾；同父且原位在下标之前 → 等效下标 -1，越界收敛。
    const targetSibs = siblingsOf(it.parent_id)
    let idx = it.position ?? targetSibs.length
    if (sameParent && it.position !== undefined && it.position > oldPos) idx -= 1
    idx = Math.min(Math.max(idx, 0), targetSibs.length)
    // 让位 + 落位。
    for (const sib of targetSibs) {
      if (sib.id !== t.id && sib.position >= idx) sib.position += 1
    }
    t.parent_id = it.parent_id
    t.position = idx
    t.revision += 1
    t.updated_at = new Date().toISOString()
    // children_count 是 mock 内存字段（服务端为读时计算，无此维护点）：
    // 换父时同步老/新父计数。
    if (!sameParent) {
      if (oldParent) {
        const old = tasks.get(oldParent)
        if (old) old.children_count = Math.max(old.children_count - 1, 0)
      }
      if (it.parent_id) {
        tasks.get(it.parent_id)!.children_count += 1
      }
    }
    emitEvent('task.updated', { task_id: t.id, parent_id: t.parent_id, position: t.position }, t.revision)
    moved.push(clone(t))
  }
  return moved
}

async function moveTasks(_workspaceId: string, items: TaskMoveItem[]): Promise<{ items: Task[] }> {
  await sleep()
  if (!items.length || items.length > 200) {
    err('VALIDATION_FAILED', 'items 须为 1–200 项。')
  }
  const seen = new Set<string>()
  for (const it of items) {
    if (seen.has(it.task_id)) err('VALIDATION_FAILED', 'task_id 不得重复。')
    seen.add(it.task_id)
    // 环检测：沿新 parent 向上走，遇到自己即环（与服务端 400 语义一致）。
    let cursor: string | null = it.parent_id
    for (let depth = 0; cursor && depth < 64; depth++) {
      if (cursor === it.task_id) err('VALIDATION_FAILED', '移动会造成循环父子关系。')
      cursor = tasks.get(cursor)?.parent_id ?? null
    }
  }
  return { items: applyMoveItems(items) }
}

async function listTags(_workspaceId: string): Promise<Page<Tag>> {
  await sleep()
  return { items: clone(tags), next_cursor: null }
}

async function listMembers(_workspaceId: string): Promise<Page<Member>> {
  await sleep()
  return { items: clone(DEMO_MEMBERS), next_cursor: null }
}

export const mockTaskApi: TaskApi = {
  listWorkspaceChildren,
  listTaskChildren,
  searchTasks,
  getTask,
  createRoot,
  createChild,
  updateTask,
  claimTask,
  moveTasks,
  renewLease,
  releaseLease,
  attachTaskTag,
  detachTaskTag,
  listTags,
  listMembers,
}

// ---- 事件模拟器动作（EventSimulator 组件调用；「他人」从演员池取）----

function pickOtherActor(): string {
  return OTHER_ACTORS[Math.floor(Math.random() * OTHER_ACTORS.length)]!.id
}

export function simulateOtherClaim(taskId: string): void {
  const t = tasks.get(taskId)
  if (!t || leaseActive(t)) return
  const holder = pickOtherActor()
  t.lease = { holder_actor_id: holder, expires_at: new Date(Date.now() + 300_000).toISOString() }
  t.assignee_actor_id = holder
  t.status = 'in_progress'
  t.revision += 1
  emitEvent('task.claimed', { task_id: t.id, lease_expires_at: t.lease.expires_at }, t.revision)
}

export function simulateOtherRelease(taskId: string): void {
  const t = tasks.get(taskId)
  if (!t || !t.lease) return
  t.lease = null
  t.assignee_actor_id = null
  if (t.status === 'in_progress') t.status = 'open'
  t.revision += 1
  emitEvent('task.released', { task_id: t.id }, t.revision)
}

export function simulateLeaseExpire(taskId: string): void {
  const t = tasks.get(taskId)
  if (!t || !t.lease) return
  const holder = t.lease.holder_actor_id
  t.lease.expires_at = new Date(Date.now() - 1000).toISOString()
  applyLeaseLoss(t, holder)
}

export function simulateOtherUpdate(taskId: string): void {
  const t = tasks.get(taskId)
  if (!t) return
  t.title = `${t.title}（他人已修改）`
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id }, t.revision)
}

export function simulateCreateTask(parentId: string | null): void {
  const stamp = new Date().toLocaleTimeString()
  createInContainer(parentId, {
    title: parentId ? `模拟事件：新建子任务 ${stamp}` : `模拟事件：新建根任务 ${stamp}`,
  })
}

export function simulateRenameTag(tagId: string): void {
  const tag = tags.find((cand) => cand.id === tagId)
  if (!tag) return
  tag.name = `${tag.name}-v2`
  emitEvent('tag.renamed', { tag_id: tag.id, name: tag.name, action: 'rename' })
}

export function simulateDeleteTag(tagId: string): void {
  const tag = tags.find((cand) => cand.id === tagId)
  if (!tag) return
  tags.splice(tags.indexOf(tag), 1)
  emitEvent('tag.deleted', { tag_id: tag.id, name: tag.name, action: 'delete' })
}

export function simulateSnapshotRequired(): void {
  emitEvent('snapshot.required', { reason: 'cursor_expired', retention_hours: 24 })
}
