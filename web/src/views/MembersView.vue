<!-- 成员与凭证管理（S7-7）：成员列表 + 角色变更（owner 晋升走审批）+
     agent/service 创建 + credential 签发（明文仅一次展示）/ 撤销。
     - 角色变更：PATCH members 端点拒绝 owner（服务端 400，architecture §22
       状态机）；选「owner（需审批晋升）」时前端改走 POST approvals
       （membership.promote_owner），裁决在裁决队列（ApprovalsView）完成。
       能力收敛用服务端数据：成员列表里自己的 role=owner 才渲染角色控件，
       非管理视角只读展示（授权仍由服务端强制）。
     - agent 卡：listAgents 403/404 即无 agent:manage（maintainer/owner），
       整卡隐藏（fail closed，InvitationsCard 同惯例）。
     - 凭证：签发响应的明文 secret 仅本次展示（复制 + 关闭即弃，A4）；
       契约无凭证列表端点，撤销依据 = 本会话签发登记的 credential_id，
       另提供按 ID 手动撤销兜底。签发请求带 workspace_id（授权分流，
       见 api/modules/credentials.ts 文件头）。 -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { toast } from 'vue-sonner'
import { KeyRound, Plus, Trash2 } from '@lucide/vue'
import { listMembers } from '@/api/modules/workspace'
import {
  createAgent,
  issueCredential,
  listAgents,
  requestOwnerPromotion,
  revokeCredential,
  updateMember,
} from '@/api/modules/credentials'
import type { AgentDto, MemberDto, Role } from '@/api/modules/credentials'
import { formatApiError } from '@/api/client'
import { useApiAction } from '@/composables/useApiAction'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import { shortId } from '@/lib/format'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/shared/PageHeader.vue'
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
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const workspaceId = useWorkspaceId()
const session = useSessionStore()
const { run } = useApiAction()

// ---- 展示常量 ----

const ROLE_LABELS: Record<Role, string> = {
  owner: '所有者',
  maintainer: '维护者',
  contributor: '贡献者',
  viewer: '观察者',
  agent: 'Agent',
}
const ROLE_VARIANTS: Record<Role, BadgeVariants['variant']> = {
  owner: 'default',
  maintainer: 'secondary',
  contributor: 'outline',
  viewer: 'outline',
  agent: 'outline',
}
const KIND_LABELS: Record<MemberDto['actor']['kind'], string> = {
  human: '用户',
  agent: 'Agent',
  service: 'Service',
}

// ---- 成员列表（workspace:read，任何成员可读）----

const members = ref<MemberDto[]>([])
const membersLoading = ref(false)
const membersLoaded = ref(false)
const membersError = ref<string | null>(null)

// listMembers（workspace.ts）本身 silent：失败在此内联呈现，可重试。
async function loadMembers(): Promise<void> {
  membersLoading.value = true
  try {
    const page = await listMembers(workspaceId.value)
    members.value = page.items ?? []
    membersError.value = null
  } catch (e) {
    membersError.value = formatApiError(e)
  } finally {
    membersLoading.value = false
    membersLoaded.value = true
  }
}

const myMember = computed(
  () => members.value.find((m) => m.actor.id === session.actor?.id) ?? null,
)
// 角色控件只对 workspace owner 渲染（角色事实来自服务端成员列表）。
const canManageMembers = computed(() => myMember.value?.role === 'owner')

const isSelf = (m: MemberDto): boolean => m.actor.id === session.actor?.id

// ---- 角色变更（owner 除外）+ owner 晋升审批 ----

const busyActorId = ref<string | null>(null)

async function changeRole(m: MemberDto, role: Role): Promise<void> {
  if (role === m.role) return
  if (role === 'owner') {
    // 服务端 PATCH 拒绝 owner：改走审批状态机（architecture §22）。
    promoteTarget.value = m
    return
  }
  busyActorId.value = m.actor.id
  await run(async () => {
    const updated = await updateMember(workspaceId.value, m.actor.id, role)
    m.role = updated.role
    toast.success(`${m.actor.display_name} 已设为 ${ROLE_LABELS[updated.role]}`)
  })
  busyActorId.value = null
}

const promoteTarget = ref<MemberDto | null>(null)
const promoting = ref(false)

async function confirmPromotion(): Promise<void> {
  const target = promoteTarget.value
  if (!target) return
  promoting.value = true
  try {
    await requestOwnerPromotion(workspaceId.value, target.actor.id)
    promoteTarget.value = null
    toast.success('已发起 owner 晋升审批：请在裁决队列批准执行。', { duration: 8000 })
  } catch {
    // 全局 toast 已提示（403 无 manage_members / 409 重复等）。
  } finally {
    promoting.value = false
  }
}

// ---- agent / service 列表（agent:manage；fail closed 整卡隐藏）----

const agents = ref<AgentDto[]>([])
const agentsLoading = ref(false)
// null = 探测中；false = 无 agent:manage（403/404）；true = 可见。
const canManageAgents = ref<boolean | null>(null)

async function loadAgents(): Promise<void> {
  agentsLoading.value = true
  try {
    const page = await listAgents(workspaceId.value, { silent: true })
    agents.value = page.items ?? []
    canManageAgents.value = true
  } catch {
    canManageAgents.value = false
  } finally {
    agentsLoading.value = false
  }
}

// ---- 创建 agent（display_name + kind）----

const createOpen = ref(false)
const createName = ref('')
const createKind = ref<'agent' | 'service'>('agent')
const creating = ref(false)

const createNameValid = computed(() => {
  const trimmed = createName.value.trim()
  return trimmed.length >= 1 && trimmed.length <= 200
})

function openCreate(): void {
  createName.value = ''
  createKind.value = 'agent'
  createOpen.value = true
}

async function submitCreate(): Promise<void> {
  if (!createNameValid.value || creating.value) return
  creating.value = true
  try {
    const agent = await createAgent(workspaceId.value, {
      display_name: createName.value.trim(),
      kind: createKind.value,
    })
    createOpen.value = false
    toast.success(`已创建 ${KIND_LABELS[agent.kind]}「${agent.display_name}」，下一步为其签发凭证。`)
    await loadAgents()
    openIssue(agent) // 创建的最终目的就是凭证化：顺势进入签发流程
  } catch {
    // 全局 toast 已提示；对话框保持打开供修改重试。
  } finally {
    creating.value = false
  }
}

// ---- 凭证签发（scope 词表 = architecture §10；默认勾选 = agent role bundle）----

const SCOPE_GROUPS: { label: string; scopes: string[] }[] = [
  { label: '工作区', scopes: ['workspace:read', 'workspace:write', 'workspace:manage_members'] },
  { label: '任务', scopes: ['task:read', 'task:write', 'task:claim', 'task:override'] },
  { label: '标签', scopes: ['tag:read', 'tag:write'] },
  { label: '文档', scopes: ['document:read', 'document:write'] },
  { label: '记忆', scopes: ['memory:read', 'memory:write'] },
  { label: '消息', scopes: ['message:read', 'message:send'] },
  { label: '在线状态', scopes: ['presence:write'] },
  { label: '审计', scopes: ['audit:read'] },
  { label: 'Agent 管理', scopes: ['agent:manage'] },
]
// D9：agent 与 contributor bundle 一致（credential 再按 scopes 收窄）。
const DEFAULT_SCOPES: string[] = [
  'workspace:read',
  'task:read', 'task:write', 'task:claim',
  'tag:read', 'tag:write',
  'document:read', 'document:write',
  'memory:read', 'memory:write',
  'message:read', 'message:send',
  'presence:write',
]
const TTL_OPTIONS = [
  { value: 'never', label: '永久' },
  { value: '7', label: '7 天' },
  { value: '30', label: '30 天' },
  { value: '90', label: '90 天' },
] as const

interface IssuedCred {
  credential_id: string
  scopes: string[]
  expires_at: string | null
  issued_at: string
  revoked: boolean
}

// 本会话签发登记（撤销依据；契约无凭证列表端点，credential_id 仅签发时可见）。
const issuedCreds = ref<Map<string, IssuedCred[]>>(new Map())

const issueTarget = ref<AgentDto | null>(null)
const issueScopes = ref<Set<string>>(new Set())
const issueTtl = ref<(typeof TTL_OPTIONS)[number]['value']>('never')
const issuing = ref(false)

function openIssue(agent: AgentDto): void {
  issueTarget.value = agent
  issueScopes.value = new Set(DEFAULT_SCOPES)
  issueTtl.value = 'never'
}

function toggleScope(scope: string): void {
  const next = new Set(issueScopes.value)
  if (next.has(scope)) next.delete(scope)
  else next.add(scope)
  issueScopes.value = next
}

async function submitIssue(): Promise<void> {
  const target = issueTarget.value
  if (!target || !issueScopes.value.size || issuing.value) return
  issuing.value = true
  try {
    const days = issueTtl.value === 'never' ? 0 : Number(issueTtl.value)
    const issued = await issueCredential(target.id, {
      scopes: [...issueScopes.value],
      expires_at: days ? new Date(Date.now() + days * 86_400_000).toISOString() : undefined,
      workspace_id: workspaceId.value,
    })
    issueTarget.value = null
    const record: IssuedCred = {
      credential_id: issued.credential_id,
      scopes: issued.scopes,
      expires_at: issued.expires_at ?? null,
      issued_at: new Date().toISOString(),
      revoked: false,
    }
    issuedCreds.value = new Map(issuedCreds.value).set(target.id, [
      ...(issuedCreds.value.get(target.id) ?? []),
      record,
    ])
    secretCred.value = record
    secretText.value = issued.secret // 一次性明文进入展示态
  } catch {
    // 全局 toast 已提示（403 agent:manage / 400 scopes 等）；留在对话框重试。
  } finally {
    issuing.value = false
  }
}

// ---- 一次性明文展示（关闭即弃：secret 只在本组件内存短暂存在）----

const secretText = ref<string | null>(null)
const secretCred = ref<IssuedCred | null>(null)

async function copySecret(): Promise<void> {
  if (!secretText.value) return
  await navigator.clipboard.writeText(secretText.value)
  toast.success('凭证明文已复制')
}

function closeSecret(): void {
  secretText.value = null
  secretCred.value = null
}

// ---- 凭证撤销（会话登记列表 + 按 ID 手动兜底）----

const revokeTarget = ref<AgentDto | null>(null)
const manualCredId = ref('')
const revokingId = ref<string | null>(null)

function openRevoke(agent: AgentDto): void {
  revokeTarget.value = agent
  manualCredId.value = ''
}

function credsOf(agentId: string): IssuedCred[] {
  return issuedCreds.value.get(agentId) ?? []
}

async function doRevoke(agentId: string, credentialId: string): Promise<void> {
  const id = credentialId.trim()
  if (!id || revokingId.value) return
  revokingId.value = id
  try {
    await revokeCredential(agentId, id)
    const list = (issuedCreds.value.get(agentId) ?? []).map((c) =>
      c.credential_id === id ? { ...c, revoked: true } : c,
    )
    issuedCreds.value = new Map(issuedCreds.value).set(agentId, list)
    if (manualCredId.value.trim() === id) manualCredId.value = ''
    toast.success('凭证已撤销（其 SSE 连接已被服务端断开）。')
  } catch {
    // 全局 toast 已提示（404 = 凭证不存在等）。
  } finally {
    revokingId.value = null
  }
}

// ---- 生命周期 ----

onMounted(() => {
  void loadMembers()
  void loadAgents()
})

watch(workspaceId, () => {
  members.value = []
  membersLoaded.value = false
  membersError.value = null
  agents.value = []
  canManageAgents.value = null
  issuedCreds.value = new Map()
  promoteTarget.value = null
  issueTarget.value = null
  revokeTarget.value = null
  closeSecret()
  void loadMembers()
  void loadAgents()
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <PageHeader
      title="成员与凭证"
      description="成员角色管理，以及 agent / service 的凭证签发与撤销（明文仅签发时展示一次）。"
    />

    <!-- 成员卡 -->
    <Card>
      <CardHeader class="gap-1.5">
        <CardTitle class="text-base">成员（{{ members.length }}）</CardTitle>
        <p v-if="membersLoaded && !membersError" class="text-muted-foreground text-xs">
          {{
            canManageMembers
              ? '角色变更即时生效；晋升 owner 需走审批（在裁决队列执行）。'
              : '仅 workspace 所有者可变更成员角色。'
          }}
        </p>
      </CardHeader>
      <CardContent>
        <div v-if="membersLoading && !members.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 4" :key="i" class="h-10" :style="{ width: `${92 - i * 8}%` }" />
        </div>

        <Empty v-else-if="membersError" class="border">
          <EmptyHeader>
            <EmptyTitle>成员列表加载失败。</EmptyTitle>
            <EmptyDescription>{{ membersError }}</EmptyDescription>
          </EmptyHeader>
          <Button variant="outline" size="sm" @click="loadMembers">重试</Button>
        </Empty>

        <Empty v-else-if="membersLoaded && !members.length" class="border">
          <EmptyHeader>
            <EmptyTitle>成员列表为空。</EmptyTitle>
          </EmptyHeader>
        </Empty>

        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>成员</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>角色</TableHead>
              <TableHead class="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="m in members" :key="m.actor.id">
              <TableCell class="font-medium">
                {{ m.actor.display_name }}
                <span v-if="isSelf(m)" class="text-muted-foreground">（当前账号）</span>
              </TableCell>
              <TableCell>
                <Badge v-if="m.actor.kind !== 'human'" variant="outline">
                  {{ KIND_LABELS[m.actor.kind] }}
                </Badge>
                <span v-else class="text-muted-foreground text-sm">用户</span>
              </TableCell>
              <TableCell>
                <template v-if="canManageMembers && !isSelf(m)">
                  <Select
                    :model-value="m.role"
                    :disabled="busyActorId === m.actor.id"
                    @update:model-value="(v) => changeRole(m, v as Role)"
                  >
                    <SelectTrigger class="h-8 w-44">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="viewer">观察者（只读）</SelectItem>
                      <SelectItem value="contributor">贡献者（读写）</SelectItem>
                      <SelectItem value="agent">Agent（凭证化）</SelectItem>
                      <SelectItem value="maintainer">维护者（管理）</SelectItem>
                      <SelectItem value="owner">所有者（需审批晋升）</SelectItem>
                    </SelectContent>
                  </Select>
                </template>
                <Badge v-else :variant="ROLE_VARIANTS[m.role]">{{ ROLE_LABELS[m.role] }}</Badge>
              </TableCell>
              <TableCell class="text-right">
                <code class="text-muted-foreground hidden font-mono text-xs lg:inline">
                  {{ shortId(m.actor.id) }}
                </code>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <!-- agent / 凭证卡（agent:manage 才可见） -->
    <Card v-if="canManageAgents">
      <CardHeader class="gap-1.5">
        <CardTitle class="flex items-center gap-2 text-base">
          <KeyRound class="size-4" />
          Agent 与凭证（{{ agents.length }}）
        </CardTitle>
        <p class="text-muted-foreground text-xs">
          签发的明文 secret 仅展示一次（服务端只存 sha256）；契约暂无凭证列表端点，
          撤销以签发时记录的 credential_id 为准，也可按 ID 手动撤销。
        </p>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex flex-wrap items-center gap-2">
          <Button size="sm" @click="openCreate">
            <Plus class="size-4" />
            新建 Agent
          </Button>
          <span class="text-muted-foreground ml-auto text-xs">
            已签发 {{ [...issuedCreds.values()].flat().length }} 条凭证（本会话）
          </span>
        </div>

        <div v-if="agentsLoading && !agents.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 2" :key="i" class="h-10" :style="{ width: `${90 - i * 10}%` }" />
        </div>

        <Table v-else-if="agents.length">
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>ID</TableHead>
              <TableHead class="text-right">凭证</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="a in agents" :key="a.id">
              <TableCell class="font-medium">{{ a.display_name }}</TableCell>
              <TableCell>
                <Badge variant="outline">{{ KIND_LABELS[a.kind] }}</Badge>
              </TableCell>
              <TableCell>
                <code class="text-muted-foreground font-mono text-xs">{{ shortId(a.id) }}</code>
              </TableCell>
              <TableCell class="text-right">
                <div class="flex items-center justify-end gap-2">
                  <Button variant="outline" size="sm" @click="openIssue(a)">
                    <KeyRound class="size-3.5" />
                    签发凭证
                  </Button>
                  <Button variant="outline" size="sm" @click="openRevoke(a)">
                    <Trash2 class="size-3.5" />
                    撤销凭证
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <Empty v-else class="border">
          <EmptyHeader>
            <EmptyTitle>还没有 agent / service。</EmptyTitle>
            <EmptyDescription>新建 agent 后为其签发凭证，即可接入 CLI 或自动化任务。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>

    <!-- owner 晋升审批确认 -->
    <AlertDialog
      :open="promoteTarget !== null"
      @update:open="(v: boolean) => { if (!v) promoteTarget = null }"
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>发起 owner 晋升审批</AlertDialogTitle>
          <AlertDialogDescription>
            将「{{ promoteTarget?.actor.display_name }}」晋升为所有者需要两步：
            先创建 membership.promote_owner 审批，再由所有者在
            <RouterLink
              :to="`/workspaces/${workspaceId}/approvals`"
              class="underline underline-offset-2"
            >
              裁决队列
            </RouterLink>
            批准执行。确定发起？
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel :disabled="promoting">取消</AlertDialogCancel>
          <AlertDialogAction :disabled="promoting" @click="confirmPromotion">
            {{ promoting ? '发起中…' : '发起审批' }}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <!-- 创建 agent -->
    <Dialog
      :open="createOpen"
      @update:open="(v: boolean) => { createOpen = v }"
    >
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>新建 Agent</DialogTitle>
          <DialogDescription>
            创建后立即进入凭证签发流程；agent 以 credential（ASTRAL_TOKEN）接入。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-1.5">
            <Label for="agent-name">名称</Label>
            <Input
              id="agent-name"
              v-model="createName"
              placeholder="1–200 个字符"
              :disabled="creating"
              @keydown.enter="submitCreate"
            />
            <p v-if="createName && !createNameValid" class="text-destructive text-xs">
              名称需 trim 后非空且不超过 200 字符。
            </p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select v-model="createKind" :disabled="creating">
              <SelectTrigger class="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="agent">agent（任务执行者）</SelectItem>
                <SelectItem value="service">service（外部服务）</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" :disabled="creating" @click="createOpen = false">取消</Button>
          <Button :disabled="!createNameValid || creating" @click="submitCreate">
            {{ creating ? '创建中…' : '创建' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 签发凭证（scope 勾选 + 有效期） -->
    <Dialog
      :open="issueTarget !== null"
      @update:open="(v: boolean) => { if (!v) issueTarget = null }"
    >
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>签发凭证</DialogTitle>
          <DialogDescription>
            为「{{ issueTarget?.display_name }}」签发 credential。scopes 决定该凭证的
            最终权限（architecture §10 词表，默认 = agent 角色包）。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-2">
            <Label>Scopes（已选 {{ issueScopes.size }}）</Label>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 rounded-lg border p-3 sm:grid-cols-3">
              <div v-for="group in SCOPE_GROUPS" :key="group.label" class="col-span-2 flex flex-col gap-1 sm:col-span-1">
                <span class="text-muted-foreground text-xs">{{ group.label }}</span>
                <label
                  v-for="scope in group.scopes"
                  :key="scope"
                  class="flex cursor-pointer items-center gap-1.5 text-xs"
                >
                  <input
                    type="checkbox"
                    class="accent-primary size-3.5"
                    :checked="issueScopes.has(scope)"
                    @change="toggleScope(scope)"
                  />
                  <code class="font-mono">{{ scope }}</code>
                </label>
              </div>
            </div>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>有效期</Label>
            <Select v-model="issueTtl" :disabled="issuing">
              <SelectTrigger class="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in TTL_OPTIONS" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" :disabled="issuing" @click="issueTarget = null">取消</Button>
          <Button :disabled="!issueScopes.size || issuing" @click="submitIssue">
            {{ issuing ? '签发中…' : '签发' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 一次性明文展示：关闭后不再可见 -->
    <Dialog
      :open="secretText !== null"
      @update:open="(v: boolean) => { if (!v) closeSecret() }"
    >
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>凭证明文（仅此一次）</DialogTitle>
          <DialogDescription>
            服务端只存 sha256，关闭本对话框后明文不再可见，丢失只能撤销重签。
            请立即复制并交给使用方（ASTRAL_TOKEN 或 Bearer 头）。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-2">
          <div class="flex items-center gap-2 rounded-lg border p-3">
            <code class="min-w-0 flex-1 font-mono text-sm break-all">{{ secretText }}</code>
            <Button size="sm" variant="outline" @click="copySecret">复制</Button>
          </div>
          <p v-if="secretCred" class="text-muted-foreground text-xs">
            credential_id：<code class="font-mono">{{ secretCred.credential_id }}</code>
            （已登记到本会话凭证列表，可随时撤销）
          </p>
        </div>
        <DialogFooter>
          <Button @click="closeSecret">我已保存，关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 撤销凭证：会话登记列表 + 按 ID 手动 -->
    <Dialog
      :open="revokeTarget !== null"
      @update:open="(v: boolean) => { if (!v) revokeTarget = null }"
    >
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>撤销凭证</DialogTitle>
          <DialogDescription>
            「{{ revokeTarget?.display_name }}」：撤销立即生效（SSE 连接同步断开）。
            下方为本次会话签发的凭证；其他凭证可按 credential_id 手动撤销。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-3">
          <div v-if="revokeTarget && credsOf(revokeTarget.id).length" class="flex flex-col gap-1.5">
            <div
              v-for="cred in credsOf(revokeTarget.id)"
              :key="cred.credential_id"
              class="flex flex-wrap items-center gap-2 rounded-lg border p-2.5"
              :class="cred.revoked ? 'opacity-60' : ''"
            >
              <code class="font-mono text-xs">{{ cred.credential_id }}</code>
              <Badge v-if="cred.revoked" variant="outline">已撤销</Badge>
              <span class="text-muted-foreground truncate text-xs">
                {{ cred.scopes.length }} scopes
              </span>
              <Button
                v-if="!cred.revoked"
                variant="destructive"
                size="xs"
                class="ml-auto"
                :disabled="revokingId !== null"
                @click="revokeTarget && doRevoke(revokeTarget.id, cred.credential_id)"
              >
                撤销
              </Button>
            </div>
          </div>
          <p v-else class="text-muted-foreground text-xs">本会话尚未签发过凭证。</p>
          <div class="flex items-end gap-2 border-t pt-3">
            <div class="min-w-0 flex-1">
              <Label for="manual-cred" class="text-muted-foreground mb-1 block text-xs">
                按 credential_id 撤销
              </Label>
              <Input id="manual-cred" v-model="manualCredId" placeholder="cred_…" />
            </div>
            <Button
              variant="destructive"
              size="sm"
              :disabled="!manualCredId.trim() || revokingId !== null"
              @click="revokeTarget && doRevoke(revokeTarget.id, manualCredId)"
            >
              撤销
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" @click="revokeTarget = null">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
