// Mock 演示夹具：任务树 / 标签 / 成员 / 演示身份。仅 dev + VITE_TASKS_MOCK=1 使用。
// ID 用递增的伪 UUIDv7（前缀 + 8 位递增 hex），保证 lexicographic id DESC == 创建时间倒序，
// 与服务端「集合按 id DESC（UUIDv7 创建序）」的排序语义一致。v2 容器语义：行内
// 恒带 tags 与 children_count。

import type { Actor, Capabilities, Member, Tag, Task, WellKnown } from '@/api/types'

export const DEMO_WORKSPACE_ID = 'ws_0199a000-7000-8000-8000-00000000dead'

// 伪 UUIDv7：n 越大 id 越大（创建越晚）。
export function fakeId(prefix: string, n: number): string {
  return `${prefix}_0199a0${n.toString(16).padStart(8, '0')}-7000-8000-8000-000000000000`
}

const hoursAgo = (h: number): string => new Date(Date.now() - h * 3_600_000).toISOString()
const minutesLater = (m: number): string => new Date(Date.now() + m * 60_000).toISOString()

// ---- 演示身份（session store 在后端不可达/未登录时的兜底登录态）----

export const DEMO_ACTOR: Actor = {
  id: fakeId('usr', 1),
  kind: 'human',
  display_name: '演示用户',
  bio: '任务视图 mock 模式的演示身份（VITE_TASKS_MOCK=1）。',
  avatar_url: '',
}

export const DEMO_WELL_KNOWN: WellKnown = {
  server_id: 'srv_demo0000',
  canonical_url: 'http://localhost:5173',
  api_base: '/api/v1',
  protocol_version: 2,
  min_cli_protocol_version: 2,
  auth: { device_login: true },
}

export const DEMO_CAPABILITIES: Capabilities = {
  protocol_version: 2,
  minimum_cli_version: '0.2.0',
  features: ['task_lease'],
}

// ---- 成员 / 标签 / 任务 ----

const AGENT_NOVA: Actor = {
  id: fakeId('usr', 2),
  kind: 'agent',
  display_name: 'Nova（agent）',
  bio: '',
  avatar_url: '',
}

const ALING: Actor = {
  id: fakeId('usr', 3),
  kind: 'human',
  display_name: '阿苓',
  bio: '',
  avatar_url: '',
}

export const DEMO_MEMBERS: Member[] = [
  { actor: DEMO_ACTOR, role: 'owner' },
  { actor: ALING, role: 'maintainer' },
  { actor: AGENT_NOVA, role: 'agent' },
]

const TAG_DEFS: Array<[number, string]> = [
  [10, 'backend'],
  [11, 'frontend'],
  [12, 'infra'],
  [13, 'bug'],
  [14, 'feature'],
  [15, 'docs'],
]

export const DEMO_TAGS: Tag[] = TAG_DEFS.map(([n, name]) => ({
  id: fakeId('tag', n),
  workspace_id: DEMO_WORKSPACE_ID,
  name,
}))

// 任务树：3 层深度、6 种状态、2 个活跃租约（其一 5 分钟后过期，供倒计时/过期演示）。
interface TaskDef {
  n: number
  title: string
  parent?: number
  status: Task['status']
  priority?: Task['priority']
  assignee?: Actor
  tags?: string[]
  desc?: string
  createdHoursAgo: number
  lease?: { holder: Actor; minutesLeft: number }
}

const DEFS: TaskDef[] = [
  { n: 101, title: '后端重构：审批与租约', status: 'in_progress', priority: 'high', assignee: DEMO_ACTOR, tags: ['backend'], desc: '收敛 approvals 状态机与 lease 清扫器的边界行为，统一事件出口。', createdHoursAgo: 72, lease: { holder: DEMO_ACTOR, minutesLeft: 5 } },
  { n: 102, title: 'approvals 状态机补测试', parent: 101, status: 'done', assignee: ALING, tags: ['backend', 'docs'], desc: '覆盖 approve 原子提升 / 裁决单次使用 / TTL 惰性过期。', createdHoursAgo: 60 },
  { n: 103, title: '过期路径边界用例', parent: 102, status: 'done', priority: 'low', createdHoursAgo: 58 },
  { n: 104, title: 'lease 清扫器条件删除', parent: 101, status: 'in_progress', assignee: AGENT_NOVA, tags: ['backend'], desc: '快照后已续租的行不再被误删；条件更新 + RowsAffected 校验。', createdHoursAgo: 40, lease: { holder: AGENT_NOVA, minutesLeft: 45 } },
  { n: 105, title: 'revision 冲突 UX 文案', parent: 101, status: 'open', priority: 'low', tags: ['docs'], createdHoursAgo: 30 },
  { n: 106, title: 'Web 任务树视图', status: 'in_progress', priority: 'urgent', assignee: DEMO_ACTOR, tags: ['frontend', 'feature'], desc: '逐容器懒加载 + 双栏布局：左侧缩进树 + 右侧详情面板。', createdHoursAgo: 26 },
  { n: 107, title: '双栏布局与树渲染', parent: 106, status: 'in_progress', priority: 'high', assignee: DEMO_ACTOR, tags: ['frontend'], createdHoursAgo: 24 },
  { n: 108, title: 'SSE 事件接线', parent: 106, status: 'blocked', tags: ['frontend', 'docs'], desc: 'task.* 事件防抖合并重载可见集合；snapshot.required 立即全量重拉。', createdHoursAgo: 20 },
  { n: 109, title: 'mock 数据夹具', parent: 106, status: 'done', priority: 'low', tags: ['frontend'], createdHoursAgo: 18 },
  { n: 110, title: 'CLI init 联调', status: 'review', assignee: ALING, tags: ['infra'], desc: '按 D12/D13 施工完成，等待真实服务器 + 浏览器审批手动验收。', createdHoursAgo: 48 },
  { n: 111, title: 'device flow 手动点验', parent: 110, status: 'open', priority: 'normal', createdHoursAgo: 46 },
  { n: 112, title: '部署文档修订', status: 'open', priority: 'low', tags: ['docs'], createdHoursAgo: 8 },
  { n: 113, title: 'v0 脚手架清理（归档）', status: 'cancelled', priority: 'low', createdHoursAgo: 90 },
  { n: 114, title: 'tag proposal 两步确认', status: 'done', tags: ['feature', 'backend'], desc: 'confirm_code hash 存储 + TTL 120s + 单次使用。', createdHoursAgo: 84 },
  { n: 115, title: 'presence 心跳节流', status: 'review', priority: 'normal', assignee: AGENT_NOVA, tags: ['infra'], createdHoursAgo: 12 },
]

function buildTask(def: TaskDef): Task {
  return {
    id: fakeId('tsk', def.n),
    workspace_id: DEMO_WORKSPACE_ID,
    parent_id: def.parent ? fakeId('tsk', def.parent) : null,
    title: def.title,
    description: def.desc ?? '',
    status: def.status,
    priority: def.priority ?? 'normal',
    assignee_actor_id: def.assignee?.id ?? null,
    revision: 1 + (def.lease ? 1 : 0) + (def.status !== 'open' ? 1 : 0),
    tags: (def.tags ?? []).map((name) => {
      const t = DEMO_TAGS.find((cand) => cand.name === name)
      if (!t) throw new Error(`fixture: unknown tag ${name}`)
      return t
    }),
    children_count: 0, // 由下方第二遍填充
    lease: def.lease
      ? {
          holder_actor_id: def.lease.holder.id,
          expires_at: minutesLater(def.lease.minutesLeft),
          renewed_at: hoursAgo(1),
        }
      : null,
    created_at: hoursAgo(def.createdHoursAgo),
    updated_at: hoursAgo(Math.max(def.createdHoursAgo - 2, 0)),
  }
}

export const DEMO_TASKS: Task[] = DEFS.map(buildTask)

// 第二遍：children_count 按父子关系填充。
const countById = new Map<string, number>()
for (const t of DEMO_TASKS) {
  if (t.parent_id) countById.set(t.parent_id, (countById.get(t.parent_id) ?? 0) + 1)
}
for (const t of DEMO_TASKS) t.children_count = countById.get(t.id) ?? 0

// 「模拟他人操作」用的演员池（排除当前用户在视图里的身份）。
export const OTHER_ACTORS: Actor[] = [AGENT_NOVA, ALING]
