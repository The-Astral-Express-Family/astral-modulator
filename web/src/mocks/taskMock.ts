// 任务数据源的内存实现（mock 模式，v2 容器语义）：复刻服务端已裁决的语义，
// 供设计演示与离线开发。对齐点：集合（workspace/task children）按 id DESC +
// status/tag/assignee 过滤；task-search 需 ≥1 条件（fuzzy 打分 0.85*title +
// 0.15*description）；创建 = 向容器 POST children（tags 按名解析，缺失 → 404，
// children_count 同步）；claim 原子抢租约；release 清租约+清 assignee+
// in_progress→open；attach/detach 幂等（已关联不 bump）；租约过期清扫
// （assignee 清空、in_progress→open、revision+1、task.lease.expired）。
// 错误以 `CODE: 文案` 形式的 Error 抛出（formatApiError 直接可读）。

import { useSessionStore } from '@/stores/session'
import type { TaskApi } from '@/api/taskSource'
import type { TaskCreatePayload, TaskFilterParams, TaskSearchParams, TaskUpdatePayload } from '@/api/modules/task'
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
  out.sort((a, b) => (a.id < b.id ? 1 : -1)) // id DESC = 创建序倒序
  if (params.cursor) out = out.filter((t) => t.id < params.cursor!)
  return out
}

function listWorkspaceChildren(_workspaceId: string, params: TaskFilterParams = {}): Promise<Page<Task>> {
  const roots = [...tasks.values()].filter((t) => t.parent_id === null)
  const items = applyFilters(roots, params).slice(0, params.limit ?? 50)
  return Promise.resolve({ items: items.map(clone), next_cursor: null })
}

function listTaskChildren(taskId: string, params: TaskFilterParams = {}): Promise<Page<Task>> {
  taskOrThrow(taskId)
  const kids = [...tasks.values()].filter((t) => t.parent_id === taskId)
  const items = applyFilters(kids, params).slice(0, params.limit ?? 50)
  return Promise.resolve({ items: items.map(clone), next_cursor: null })
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

function searchTasks(_workspaceId: string, params: TaskSearchParams): Promise<Page<TaskSearchHit>> {
  if (!params.regex && !params.fuzzy && !params.tag && !params.status && !params.assignee) {
    return Promise.reject(new Error('VALIDATION_FAILED: 至少需要一个过滤条件（regex/fuzzy/tag/status/assignee）。'))
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
      return Promise.reject(new Error('VALIDATION_FAILED: regex 语法无效。'))
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
  return Promise.resolve({ items, next_cursor: null })
}

function getTask(taskId: string): Promise<Task> {
  return Promise.resolve(clone(taskOrThrow(taskId)))
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

function createRoot(_workspaceId: string, payload: TaskCreatePayload): Promise<Task> {
  return Promise.resolve(clone(createInContainer(null, payload)))
}

function createChild(taskId: string, payload: TaskCreatePayload): Promise<Task> {
  return Promise.resolve(clone(createInContainer(taskId, payload)))
}

function updateTask(taskId: string, payload: TaskUpdatePayload): Promise<Task> {
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
  return Promise.resolve(clone(t))
}

function claimTask(
  taskId: string,
  payload: { expected_revision: number; lease_seconds?: number },
): Promise<{ task: Task; lease: Lease }> {
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
  return Promise.resolve({ task: clone(t), lease: clone(lease) })
}

function renewLease(taskId: string, leaseSeconds?: number): Promise<Lease> {
  const t = taskOrThrow(taskId)
  if (!leaseActive(t)) err('TASK_LEASE_EXPIRED', '租约已过期，需重新认领。')
  t.lease!.expires_at = new Date(Date.now() + (leaseSeconds ?? 300) * 1000).toISOString()
  t.lease!.renewed_at = new Date().toISOString()
  return Promise.resolve(clone(t.lease!))
}

function releaseLease(taskId: string): Promise<void> {
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
  return Promise.resolve()
}

function attachTaskTag(taskId: string, tagId: string, expectedRevision?: number): Promise<Task> {
  const t = taskOrThrow(taskId)
  const tag = tags.find((cand) => cand.id === tagId)
  if (!tag) err('NOT_FOUND', '标签不存在。')
  if (t.tags.some((cand) => cand.id === tagId)) return Promise.resolve(clone(t)) // 幂等：不 bump
  if (expectedRevision !== undefined) checkRevision(t, expectedRevision)
  t.tags.push(clone(tag))
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id, tag_change: 'attach', tag_id: tagId }, t.revision)
  return Promise.resolve(clone(t))
}

function detachTaskTag(taskId: string, tagId: string): Promise<void> {
  const t = taskOrThrow(taskId)
  const before = t.tags.length
  t.tags = t.tags.filter((cand) => cand.id !== tagId)
  if (t.tags.length === before) return Promise.resolve() // 幂等：不 bump
  t.revision += 1
  t.updated_at = new Date().toISOString()
  emitEvent('task.updated', { task_id: t.id, tag_change: 'detach', tag_id: tagId }, t.revision)
  return Promise.resolve()
}

function listTags(_workspaceId: string): Promise<Page<Tag>> {
  return Promise.resolve({ items: clone(tags), next_cursor: null })
}

function listMembers(_workspaceId: string): Promise<Page<Member>> {
  return Promise.resolve({ items: clone(DEMO_MEMBERS), next_cursor: null })
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
