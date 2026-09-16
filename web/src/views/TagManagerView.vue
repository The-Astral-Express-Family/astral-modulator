<!-- 标签管理（S7-2）：workspace tag 列表 + 两步确认（proposal → confirm）的
     创建/改名/删除 + 每个 tag 的挂载任务入口。
     - 契约（openapi tags 段）：动作没有直改端点，rename/delete 一律经
       tag-proposals（action=rename|delete + target_tag_id）两步确认落地；
       confirm_code 一次性、TTL 120s，服务端 403（码不匹配）/409（过期已用/
       重名）由全局 toast 展示，失败后丢弃 proposal 由用户重新发起。
     - 挂载任务入口：TaskTreeView 的 tag 过滤是组件内部状态、不支持路由
       query 深链，故按契约 task-search?tag= 端点在行内展开挂载该 tag 的
       任务列表（挂载/摘除操作本身在任务树详情面板完成）。 -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { toast } from 'vue-sonner'
import { ChevronDown, ChevronRight, Pencil, Plus, Tags, Trash2 } from '@lucide/vue'
import { confirmTagProposal, listTags, proposeTag } from '@/api/modules/tag'
import type { TagDto, TagProposal } from '@/api/modules/tag'
import { searchTasks } from '@/api/modules/task'
import type { TaskSearchHit } from '@/api/types'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import PageHeader from '@/components/shared/PageHeader.vue'
import TaskStatusBadge from '@/components/tasks/TaskStatusBadge.vue'
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
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
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
import { Skeleton } from '@/components/ui/skeleton'

const workspaceId = useWorkspaceId()

// ---- 标签列表（服务端按成员规模有界一次返回；next_cursor 仍照常消费）----

const PAGE_LIMIT = 200
const TASK_PAGE_LIMIT = 50

const tags = ref<TagDto[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)

async function load(opts: { append?: boolean; silent?: boolean } = {}): Promise<void> {
  loading.value = true
  try {
    const page = await listTags(
      workspaceId.value,
      opts.append && nextCursor.value ? { limit: PAGE_LIMIT, cursor: nextCursor.value } : { limit: PAGE_LIMIT },
      { silent: opts.silent },
    )
    const items = page.items ?? [] // 生成类型 items 可选（allOf 合并形态）
    tags.value = opts.append ? [...tags.value, ...items] : items
    nextCursor.value = page.next_cursor
  } catch {
    // 失败已由全局拦截器 toast（silent 时为后台刷新，不打扰）。
  } finally {
    loading.value = false
    loaded.value = true
  }
}

// ---- 两步确认流：发起（create/rename 收集名字；delete 直接发起到 tag 现名）----

type NameDialog = { mode: 'create' } | { mode: 'rename'; tag: TagDto }

const nameDialog = ref<NameDialog | null>(null)
const nameDraft = ref('')
const proposing = ref(false)

// proposal 就绪后的确认面（ApprovalsView 的 AlertDialog 异步流模式）。
// proposalName = 发起提案时提交的名字原文，confirm 需原样回传（服务端按
// canonical 名比对）。
const proposal = ref<TagProposal | null>(null)
const proposalAction = ref<'create' | 'rename' | 'delete'>('create')
const proposalName = ref('')
const proposalLabel = ref('')
const confirmOpen = ref(false)
const confirming = ref(false)

// 客户端预校验与服务端 ValidateTagName 同规则：trim 非空、≤64 字符、无控制字符。
const nameValid = computed(() => {
  const raw = nameDraft.value
  const trimmed = raw.trim()
  if (!trimmed || trimmed.length > 64) return false
  return ![...raw].some((ch) => ch < ' ' || ch === '\x7f')
})

function openCreate(): void {
  nameDialog.value = { mode: 'create' }
  nameDraft.value = ''
}

function openRename(tag: TagDto): void {
  nameDialog.value = { mode: 'rename', tag }
  nameDraft.value = tag.name
}

function actionLabel(action: 'create' | 'rename' | 'delete'): string {
  return { create: '创建', rename: '重命名', delete: '删除' }[action]
}

// 第一步：发起 proposal。失败（含 create 重名 409 TAG_ALREADY_EXISTS）由全局
// 拦截器 toast，对话框保持打开供修改后重试。
async function submitProposal(): Promise<void> {
  const dialog = nameDialog.value
  if (!dialog || !nameValid.value) return
  const name = nameDraft.value.trim()
  proposing.value = true
  try {
    const result = await proposeTag(workspaceId.value, {
      action: dialog.mode,
      name,
      ...(dialog.mode === 'rename' ? { target_tag_id: dialog.tag.id } : {}),
    })
    nameDialog.value = null
    openConfirm(dialog.mode, name, result, dialog.mode === 'rename' ? dialog.tag.name : undefined)
  } catch {
    // 全局 toast 已提示；留在输入对话框。
  } finally {
    proposing.value = false
  }
}

function requestDelete(tag: TagDto): void {
  proposing.value = true
  proposeTag(workspaceId.value, { action: 'delete', name: tag.name, target_tag_id: tag.id })
    .then((result) => openConfirm('delete', tag.name, result))
    .catch(() => {
      // 全局 toast 已提示。
    })
    .finally(() => {
      proposing.value = false
    })
}

function openConfirm(
  action: 'create' | 'rename' | 'delete',
  name: string,
  result: TagProposal,
  oldName?: string,
): void {
  proposal.value = result
  proposalAction.value = action
  proposalName.value = name
  proposalLabel.value =
    action === 'delete'
      ? `将删除标签「${name}」`
      : action === 'rename' && oldName
        ? `将把「${oldName}」重命名为「${name}」`
        : `将创建标签「${name}」`
  confirmOpen.value = true
}

// 第二步：confirm。错误（403 码不匹配 / 409 过期或已用 / 409 重名）由全局
// toast 展示；confirm_code 单次有效，失败后丢弃 proposal 重新发起。
async function confirmProposal(): Promise<void> {
  const pending = proposal.value
  if (!pending) return
  confirming.value = true
  try {
    await confirmTagProposal(pending.proposal_id, {
      confirm_code: pending.confirm_code,
      name: proposalName.value,
    })
    toast.success(`已${actionLabel(proposalAction.value)}标签。`)
    confirmOpen.value = false
    proposal.value = null
    await load({ silent: true })
    taskLists.value = new Map() // tag 名可能已变，挂载任务缓存整体作废
  } catch {
    // 一次性码已消费或已过期：关闭确认面，列表静默刷新（状态可能已漂移）。
    confirmOpen.value = false
    proposal.value = null
    void load({ silent: true })
  } finally {
    confirming.value = false
  }
}

const fmtExpiry = (iso: string): string => new Date(iso).toLocaleTimeString()

// ---- 挂载任务入口（task-search?tag= 行内展开）----

interface TaskListState {
  items: TaskSearchHit[]
  nextCursor: string | null
  loading: boolean
}

const taskLists = ref<Map<string, TaskListState>>(new Map())
const expandedTagIds = ref<Set<string>>(new Set())

function toggleTasks(tag: TagDto): void {
  const next = new Set(expandedTagIds.value)
  if (next.has(tag.id)) {
    next.delete(tag.id)
  } else {
    next.add(tag.id)
    if (!taskLists.value.has(tag.id)) void loadTasks(tag)
  }
  expandedTagIds.value = next
}

async function loadTasks(tag: TagDto, append = false): Promise<void> {
  const current = taskLists.value.get(tag.id)
  const params = { tag: tag.name, limit: TASK_PAGE_LIMIT }
  const state: TaskListState = current
    ? { ...current, loading: true }
    : { items: [], nextCursor: null, loading: true }
  taskLists.value = new Map(taskLists.value).set(tag.id, state)
  try {
    const page = await searchTasks(workspaceId.value, {
      ...params,
      ...(append && current?.nextCursor ? { cursor: current.nextCursor } : {}),
    })
    const fresh = page.items ?? []
    const merged = append && current ? [...current.items, ...fresh] : fresh
    taskLists.value = new Map(taskLists.value).set(tag.id, {
      items: merged,
      nextCursor: page.next_cursor,
      loading: false,
    })
  } catch {
    // 失败已由全局拦截器 toast；保留已加载内容并停转圈。
    taskLists.value = new Map(taskLists.value).set(tag.id, { ...state, loading: false })
  }
}

// ---- 生命周期 ----

onMounted(() => {
  void load()
})

watch(workspaceId, () => {
  tags.value = []
  nextCursor.value = null
  loaded.value = false
  taskLists.value = new Map()
  expandedTagIds.value = new Set()
  proposal.value = null
  confirmOpen.value = false
  nameDialog.value = null
  void load()
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader
        title="标签管理"
        description="标签的创建 / 改名 / 删除均走两步确认：先发起提案，再用一次性校验码确认落地。"
      />
      <Button size="sm" :disabled="proposing" @click="openCreate">
        <Plus class="size-4" />
        新建标签
      </Button>
    </div>

    <Card>
      <CardContent class="flex flex-col gap-2 p-4">
        <div v-if="loading && !tags.length" class="flex flex-col gap-2">
          <Skeleton v-for="i in 4" :key="i" class="h-10" :style="{ width: `${92 - i * 8}%` }" />
        </div>

        <template v-else-if="tags.length">
          <div v-for="tag in tags" :key="tag.id" class="flex flex-col gap-1">
            <div class="hover:bg-muted/40 flex items-center gap-2 rounded-lg px-2 py-1.5">
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="expandedTagIds.has(tag.id) ? '收起挂载任务' : '展开挂载任务'"
                @click="toggleTasks(tag)"
              >
                <component :is="expandedTagIds.has(tag.id) ? ChevronDown : ChevronRight" class="size-4" />
              </Button>
              <Badge variant="secondary" class="max-w-64">
                <Tags class="size-3" />
                <span class="truncate">{{ tag.name }}</span>
              </Badge>
              <code class="text-muted-foreground hidden truncate font-mono text-xs md:inline">{{ tag.id }}</code>
              <span class="ml-auto flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="重命名"
                  :disabled="proposing"
                  @click="openRename(tag)"
                >
                  <Pencil class="size-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="删除"
                  :disabled="proposing"
                  @click="requestDelete(tag)"
                >
                  <Trash2 class="size-3.5" />
                </Button>
              </span>
            </div>

            <!-- 挂载任务行内列表（TaskTreeView 不支持 tag 深链，入口在此落地）-->
            <div
              v-if="expandedTagIds.has(tag.id)"
              class="bg-muted/30 ml-9 flex flex-col gap-1 rounded-lg border p-2"
            >
              <template v-if="taskLists.get(tag.id)?.items.length">
                <div
                  v-for="hit in taskLists.get(tag.id)!.items"
                  :key="hit.id"
                  class="flex items-center gap-2 rounded-md px-1.5 py-1 text-sm"
                >
                  <TaskStatusBadge :status="hit.status" />
                  <span class="min-w-0 flex-1 truncate">{{ hit.title }}</span>
                  <code class="text-muted-foreground hidden shrink-0 font-mono text-xs lg:inline">
                    {{ hit.id.slice(0, 12) }}…
                  </code>
                </div>
              </template>
              <p v-else-if="!taskLists.get(tag.id)?.loading" class="text-muted-foreground px-1.5 py-1 text-xs">
                没有任务挂载该标签。
              </p>
              <div v-if="taskLists.get(tag.id)?.loading" class="flex flex-col gap-1.5 px-1.5 py-1">
                <Skeleton class="h-5 w-2/3" />
                <Skeleton class="h-5 w-1/2" />
              </div>
              <div class="flex items-center gap-2 px-1.5 pt-1">
                <Button
                  v-if="taskLists.get(tag.id)?.nextCursor"
                  variant="outline"
                  size="xs"
                  :disabled="taskLists.get(tag.id)?.loading"
                  @click="loadTasks(tag, true)"
                >
                  加载更多
                </Button>
                <RouterLink
                  :to="`/workspaces/${workspaceId}/tasks`"
                  class="text-muted-foreground hover:text-foreground text-xs underline-offset-2 hover:underline"
                >
                  去任务树挂载 / 摘除标签 →
                </RouterLink>
              </div>
            </div>
          </div>

          <Button
            v-if="nextCursor"
            variant="outline"
            size="sm"
            class="self-start"
            :disabled="loading"
            @click="load({ append: true })"
          >
            加载更多
          </Button>
        </template>

        <Empty v-else-if="loaded" class="border">
          <EmptyHeader>
            <EmptyTitle>工作区还没有标签。</EmptyTitle>
            <EmptyDescription>
              新建标签后，可在任务树的详情面板把它挂载到任务上。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </CardContent>
    </Card>

    <!-- 第一步输入面：create / rename 共用 -->
    <Dialog
      :open="nameDialog !== null"
      @update:open="(v: boolean) => { if (!v) nameDialog = null }"
    >
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{{ nameDialog?.mode === 'rename' ? '重命名标签' : '新建标签' }}</DialogTitle>
          <DialogDescription>
            {{
              nameDialog?.mode === 'rename'
                ? '提交后服务端下发一次性校验码，第二步确认才落地。'
                : '提交后服务端下发一次性校验码，第二步确认才创建。'
            }}
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-1.5">
          <Label for="tag-name">标签名</Label>
          <Input
            id="tag-name"
            v-model="nameDraft"
            placeholder="1–64 个字符"
            :disabled="proposing"
            @keydown.enter="submitProposal"
          />
          <p v-if="nameDraft && !nameValid" class="text-destructive text-xs">
            标签名需 trim 后非空、不超过 64 字符、不含控制字符。
          </p>
        </div>
        <DialogFooter>
          <Button variant="ghost" :disabled="proposing" @click="nameDialog = null">取消</Button>
          <Button :disabled="!nameValid || proposing" @click="submitProposal">
            {{ proposing ? '发起提案…' : '发起提案' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 第二步确认面：展示一次性码 + 全量已有标签（供比对三思）-->
    <AlertDialog :open="confirmOpen" @update:open="confirmOpen = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>确认{{ actionLabel(proposalAction) }}标签</AlertDialogTitle>
          <AlertDialogDescription>
            {{ proposalLabel }}。校验码 2 分钟内单次有效，确认后立即生效。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div class="flex flex-col gap-3">
          <div class="flex items-center justify-between gap-3 rounded-lg border p-3">
            <div class="flex flex-col">
              <span class="text-muted-foreground text-xs">一次性校验码（自动提交）</span>
              <code v-if="proposal" class="font-mono text-2xl font-semibold tracking-[0.3em]">
                {{ proposal.confirm_code }}
              </code>
            </div>
            <span v-if="proposal" class="text-muted-foreground text-xs">
              有效期至 {{ fmtExpiry(proposal.expires_at) }}
            </span>
          </div>
          <div v-if="proposal?.existing_tags.length" class="flex flex-col gap-1.5">
            <span class="text-muted-foreground text-xs">
              工作区已有标签（{{ proposal.existing_tags.length }} 个，语义相近请三思）：
            </span>
            <div class="flex flex-wrap gap-1.5">
              <Badge v-for="t in proposal.existing_tags" :key="t.id" variant="outline">
                {{ t.name }}
              </Badge>
            </div>
          </div>
          <p class="text-muted-foreground text-xs">
            码不匹配（403）、过期或已使用（409）、重名冲突（409）时需重新发起提案。
          </p>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel :disabled="confirming">取消</AlertDialogCancel>
          <AlertDialogAction
            :variant="proposalAction === 'delete' ? 'destructive' : 'default'"
            :disabled="confirming"
            @click="confirmProposal"
          >
            {{ confirming ? '确认中…' : `确认${actionLabel(proposalAction)}` }}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
