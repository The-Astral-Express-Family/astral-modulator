<!-- 冲突解决（S7-6）：document_conflicts 列表 + 详情双栏对比 + resolve 四选一。
     - 列表：status=open|resolved|all 过滤（缺省 open），created_at DESC + id
       游标「加载更多」；支持 ?conflict=<id> 深链直达详情（DocumentsView 活动
       栏跳转入口）。
     - 详情双栏 ours | theirs：ours = push 方提交的完整内容（hash/无 revision），
       theirs = 冲突落档时服务端持有的完整内容（revision + hash）。delete 意图
       工件（delete-vs-edit 的 ours 侧）无内容与 hash（服务端 ours_hash
       omitempty + base_hash 空串，T4 定义）——显示「删除意图」标记而非空文本。
     - resolve 四选一：ours（以 ours 落新 revision；delete 工件则落 tombstone）/
       theirs（保留服务端当前，仅关闭）/ merged（客户端已合并的结果）/ manual
       （人工定稿）；merged|manual 弹出内容编辑框必填（缺失 400）。
     - SSE document.conflict / document.updated（resolve 内容分支会发）防抖
       静默刷新列表与已选详情，MessagesView 同精神。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { toast } from 'vue-sonner'
import { Activity, Check, Columns2, FileWarning } from '@lucide/vue'
import { getConflict, listConflicts, resolveConflict } from '@/api/modules/documents'
import type {
  ConflictResolution,
  DocumentConflictDetailDto,
  DocumentConflictDto,
} from '@/api/modules/documents'
import { useEventStream } from '@/composables/useEventStream'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import PageHeader from '@/components/shared/PageHeader.vue'
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
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'

const workspaceId = useWorkspaceId()
const route = useRoute()

const PAGE_LIMIT = 50

// ---- SSE 实时刷新（新冲突落档 / resolve 落地后对齐）----

const { state: sseState } = useEventStream(workspaceId, { onEvent: onSseEvent })
let reloadTimer: ReturnType<typeof setTimeout> | null = null

const SSE_VARIANTS: Record<string, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}

function onSseEvent(env: { type: string }): void {
  if (env.type !== 'document.conflict' && env.type !== 'document.updated') return
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => {
    reloadTimer = null
    void loadList({ silent: true })
    if (selectedId.value) void loadDetail({ silent: true })
  }, 800)
}

onUnmounted(() => {
  if (reloadTimer) clearTimeout(reloadTimer)
})

// ---- 列表（status 过滤 + id 游标加载更多）----

type StatusFilter = 'open' | 'resolved' | 'all'

const statusFilter = ref<StatusFilter>('open')
const STATUS_CHIPS = [
  { value: 'open', label: '未解决' },
  { value: 'resolved', label: '已解决' },
  { value: 'all', label: '全部' },
] as const

const conflicts = ref<DocumentConflictDto[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)

async function loadList(opts: { append?: boolean; silent?: boolean } = {}): Promise<void> {
  loading.value = true
  try {
    const page = await listConflicts(
      workspaceId.value,
      {
        limit: PAGE_LIMIT,
        status: statusFilter.value,
        ...(opts.append && nextCursor.value ? { cursor: nextCursor.value } : {}),
      },
      { silent: opts.silent },
    )
    const fresh = page.items ?? [] // 生成类型 items 可选
    conflicts.value = opts.append ? [...conflicts.value, ...fresh] : fresh
    nextCursor.value = page.next_cursor ?? null
  } catch {
    // 失败已由全局拦截器 toast（silent 时为 SSE 防抖刷新，不打扰）。
  } finally {
    loading.value = false
    loaded.value = true
  }
}

function setStatus(next: StatusFilter): void {
  if (statusFilter.value === next) return
  statusFilter.value = next
  nextCursor.value = null
  void loadList()
}

// ---- 详情（双方全文对比）----

const selectedId = ref('')
const detail = ref<DocumentConflictDetailDto | null>(null)
const detailLoading = ref(false)

function selectConflict(id: string): void {
  if (selectedId.value === id && detail.value) return
  selectedId.value = id
  detail.value = null
  void loadDetail()
}

async function loadDetail(opts: { silent?: boolean } = {}): Promise<void> {
  if (!selectedId.value) return
  detailLoading.value = true
  try {
    detail.value = await getConflict(workspaceId.value, selectedId.value, { silent: opts.silent })
  } catch {
    // 404 等已由全局拦截器 toast；保留选中 id，详情区显示空态。
    detail.value = null
  } finally {
    detailLoading.value = false
  }
}

// delete 意图工件（delete-vs-edit 的 ours 侧）：服务端 ours_hash omitted、
// base_hash 为空串（T4 定义，document/dto.go 注释），ours_content 为空串。
// schema 侧两类字段均可选/可空，判据 = ours_hash 缺失。
const isDeleteIntent = computed(
  () => !!detail.value && (detail.value.ours_hash === undefined || detail.value.ours_hash === ''),
)

const isOpen = computed(() => detail.value?.status === 'open')

// ---- resolve 四选一 ----

const RESOLUTION_LABELS: Record<ConflictResolution, string> = {
  ours: '采用推送方（ours）',
  theirs: '保留服务端（theirs）',
  merged: '提交合并结果（merged）',
  manual: '人工定稿（manual）',
}

const resolving = ref<ConflictResolution | null>(null)
const resolveDialog = ref<{ mode: 'merged' | 'manual' } | null>(null)
const resolveDraft = ref('')

const draftValid = computed(() => resolveDraft.value.length > 0)

// merged 预填推送方内容（ours 为删除意图时退服务端当前——合并的起点是待落地
// 一侧）；manual 预填服务端当前（人工定稿从现状出发）。均可自由改写。
function openResolveDialog(mode: 'merged' | 'manual'): void {
  const d = detail.value
  if (!d) return
  resolveDialog.value = { mode }
  resolveDraft.value =
    mode === 'merged' && !isDeleteIntent.value ? d.ours_content : d.theirs_content
}

async function doResolve(resolution: ConflictResolution, content?: string): Promise<void> {
  const d = detail.value
  if (!d || resolving.value) return
  resolving.value = resolution
  try {
    const doc = await resolveConflict(workspaceId.value, d.id, {
      resolution,
      ...(content !== undefined ? { content } : {}),
    })
    toast.success(
      `冲突已解决（${RESOLUTION_LABELS[resolution]}），文档现于 r${doc.revision}${doc.deleted ? '（tombstone）' : ''}。`,
    )
    resolveDialog.value = null
    await Promise.all([loadList(), loadDetail()])
  } catch {
    // 已解决再 resolve（409 already_resolved）等已由全局 toast；刷新对齐真实状态。
    await Promise.all([loadList({ silent: true }), loadDetail({ silent: true })])
  } finally {
    resolving.value = null
  }
}

// ---- 展示辅助 ----

const fmtTime = (iso: string): string => new Date(iso).toLocaleString()
// theirs_hash / ours_hash 在生成类型中可选（ours 对 delete 意图省略）。
const shortHash = (hash: string | null | undefined): string => (hash ? `${hash.slice(0, 14)}…` : '—')

function resolutionBadgeVariant(
  resolution: ConflictResolution | null | undefined,
): BadgeVariants['variant'] {
  if (resolution === 'merged' || resolution === 'manual') return 'default'
  return 'secondary'
}

// ---- 深链（?conflict=<id>）与生命周期 ----

onMounted(() => {
  void loadList()
  const deep = route.query.conflict
  const id = Array.isArray(deep) ? deep[0] : deep
  if (id) selectConflict(String(id))
})

watch(workspaceId, () => {
  conflicts.value = []
  nextCursor.value = null
  loaded.value = false
  statusFilter.value = 'open'
  selectedId.value = ''
  detail.value = null
  resolveDialog.value = null
  void loadList()
})

// 深链参数变化（DocumentsView 活动栏跳转）时切换详情。
watch(
  () => route.query.conflict,
  (deep) => {
    const id = Array.isArray(deep) ? deep[0] : deep
    if (id && String(id) !== selectedId.value) selectConflict(String(id))
  },
)
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader
        title="冲突解决"
        description="push/delete 与远端版本失配时落冲突工件；对比双方全文后选择 ours / theirs / merged / manual 落地。"
      />
      <Badge :variant="SSE_VARIANTS[sseState] ?? 'outline'">
        <Activity class="size-3" />
        SSE {{ sseState }}
      </Badge>
    </div>

    <div class="grid gap-4 lg:grid-cols-12">
      <!-- 列表 -->
      <Card class="lg:col-span-5">
        <CardHeader class="gap-1.5">
          <CardTitle class="flex items-center gap-2 text-base">
            <FileWarning class="size-4" />
            冲突列表
          </CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-2">
          <div class="flex flex-wrap items-center gap-2">
            <div class="border-input flex gap-0.5 rounded-lg border p-0.5">
              <Button
                v-for="chip in STATUS_CHIPS"
                :key="chip.value"
                :variant="statusFilter === chip.value ? 'default' : 'ghost'"
                size="xs"
                @click="setStatus(chip.value)"
              >
                {{ chip.label }}
              </Button>
            </div>
            <span class="text-muted-foreground ml-auto text-xs">
              已加载 {{ conflicts.length }} 条（时间降序）
            </span>
          </div>

          <div v-if="loading && !conflicts.length" class="flex flex-col gap-2">
            <Skeleton v-for="i in 4" :key="i" class="h-9" :style="{ width: `${92 - i * 6}%` }" />
          </div>

          <template v-else-if="conflicts.length">
            <button
              v-for="c in conflicts"
              :key="c.id"
              type="button"
              class="flex flex-wrap items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm"
              :class="c.id === selectedId ? 'bg-primary/10' : 'hover:bg-muted/40'"
              @click="selectConflict(c.id)"
            >
              <Badge :variant="c.status === 'open' ? 'destructive' : 'outline'" class="shrink-0">
                {{ c.status === 'open' ? '未解决' : '已解决' }}
              </Badge>
              <span class="min-w-0 flex-1 truncate font-mono text-xs">{{ c.path }}</span>
              <span class="text-muted-foreground shrink-0 font-mono text-xs">
                r{{ c.base_revision }} → r{{ c.theirs_revision }}
              </span>
              <Badge
                v-if="c.resolution"
                :variant="resolutionBadgeVariant(c.resolution)"
                class="shrink-0"
              >
                <Check class="size-3" />
                {{ c.resolution }}
              </Badge>
              <span class="text-muted-foreground hidden shrink-0 text-xs md:inline">
                {{ fmtTime(c.created_at) }}
              </span>
            </button>
            <Button
              v-if="nextCursor"
              variant="outline"
              size="sm"
              class="self-start"
              :disabled="loading"
              @click="loadList({ append: true })"
            >
              加载更多
            </Button>
          </template>

          <Empty v-else-if="loaded" class="border">
            <EmptyHeader>
              <EmptyTitle>
                {{ statusFilter === 'open' ? '没有未解决冲突。' : '没有匹配的冲突。' }}
              </EmptyTitle>
              <EmptyDescription>
                {{
                  statusFilter === 'open'
                    ? '文档同步一切正常；push/delete 与远端失配时冲突会出现在这里。'
                    : '换个状态过滤试试。'
                }}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>

      <!-- 详情双栏对比 + resolve -->
      <Card class="lg:col-span-7">
        <CardHeader class="gap-1.5">
          <CardTitle class="flex min-w-0 items-center gap-2 text-base">
            <Columns2 class="size-4 shrink-0" />
            <template v-if="detail">
              <span class="truncate font-mono text-sm">{{ detail.path }}</span>
              <Badge :variant="detail.status === 'open' ? 'destructive' : 'outline'" class="shrink-0">
                {{ detail.status === 'open' ? '未解决' : '已解决' }}
              </Badge>
            </template>
            <template v-else>冲突详情</template>
          </CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <template v-if="selectedId">
            <div v-if="detailLoading && !detail" class="flex flex-col gap-2">
              <Skeleton class="h-5 w-2/3" />
              <Skeleton class="h-32 w-full" />
            </div>

            <template v-else-if="detail">
              <!-- base 指针元信息 -->
              <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
                <span class="text-muted-foreground">
                  基线
                  <code class="font-mono">r{{ detail.base_revision }}</code>
                  <code class="text-muted-foreground font-mono" :title="detail.base_hash">
                    {{ detail.base_hash ? shortHash(detail.base_hash) : '（delete 意图无 hash 指针）' }}
                  </code>
                </span>
                <span class="text-muted-foreground">发生于 {{ fmtTime(detail.created_at) }}</span>
                <span v-if="detail.status === 'resolved'" class="text-muted-foreground">
                  已以 {{ RESOLUTION_LABELS[detail.resolution ?? 'ours'] }} 解决
                  <template v-if="detail.resolved_at">· {{ fmtTime(detail.resolved_at) }}</template>
                </span>
              </div>

              <!-- 双栏 ours | theirs -->
              <div class="grid gap-3 md:grid-cols-2">
                <div class="flex flex-col gap-1.5 rounded-lg border p-3">
                  <div class="flex flex-wrap items-center gap-2 text-xs">
                    <span class="font-medium">ours（推送方）</span>
                    <span class="text-muted-foreground font-mono" :title="detail.ours_hash">
                      {{ detail.ours_hash ? shortHash(detail.ours_hash) : '' }}
                    </span>
                    <Badge v-if="isDeleteIntent" variant="destructive" class="ml-auto">删除意图</Badge>
                  </div>
                  <p v-if="isDeleteIntent" class="text-muted-foreground text-xs">
                    推送方当时的意图是删除该文档（版本化 delete 与远端编辑失配）；采用
                    ours 将落 tombstone。
                  </p>
                  <pre
                    v-else
                    class="bg-muted/40 max-h-64 overflow-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap break-all"
                  >{{ detail.ours_content }}</pre>
                </div>
                <div class="flex flex-col gap-1.5 rounded-lg border p-3">
                  <div class="flex flex-wrap items-center gap-2 text-xs">
                    <span class="font-medium">theirs（服务端当前）</span>
                    <span class="text-muted-foreground font-mono">
                      r{{ detail.theirs_revision }}
                    </span>
                    <span class="text-muted-foreground font-mono" :title="detail.theirs_hash">
                      {{ shortHash(detail.theirs_hash) }}
                    </span>
                  </div>
                  <pre class="bg-muted/40 max-h-64 overflow-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap break-all">{{ detail.theirs_content }}</pre>
                </div>
              </div>

              <!-- resolve 四选一（仅 open）-->
              <div v-if="isOpen" class="flex flex-col gap-2 border-t pt-3">
                <span class="text-muted-foreground text-xs">
                  ours=以推送方内容落新 revision（删除意图则落 tombstone）；theirs=保留
                  服务端当前并关闭；merged/manual=提交最终内容（必填）。
                </span>
                <div class="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    :variant="isDeleteIntent ? 'destructive' : 'default'"
                    :disabled="!!resolving"
                    @click="doResolve('ours')"
                  >
                    {{ resolving === 'ours' ? '落地中…' : isDeleteIntent ? '采用 ours（删除）' : '采用 ours' }}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    :disabled="!!resolving"
                    @click="doResolve('theirs')"
                  >
                    {{ resolving === 'theirs' ? '关闭中…' : '保留 theirs' }}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    :disabled="!!resolving"
                    @click="openResolveDialog('merged')"
                  >
                    提交合并（merged）
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    :disabled="!!resolving"
                    @click="openResolveDialog('manual')"
                  >
                    人工定稿（manual）
                  </Button>
                </div>
              </div>
            </template>

            <Empty v-else class="border">
              <EmptyHeader>
                <EmptyTitle>详情不可见。</EmptyTitle>
                <EmptyDescription>
                  读取失败（可能已被清理），可重选左侧冲突重试。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          </template>

          <Empty v-else class="border">
            <EmptyHeader>
              <EmptyTitle>选择一条冲突。</EmptyTitle>
              <EmptyDescription>
                详情含双方完整内容；对比后选择落地方式（merged / manual 需提交最终内容）。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>
    </div>

    <!-- merged / manual 内容编辑框（content 必填）-->
    <Dialog
      :open="resolveDialog !== null"
      @update:open="(v: boolean) => { if (!v) resolveDialog = null }"
    >
      <DialogContent class="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {{ resolveDialog?.mode === 'merged' ? '提交合并结果（merged）' : '人工定稿（manual）' }}
          </DialogTitle>
          <DialogDescription>
            {{
              resolveDialog?.mode === 'merged'
                ? '粘贴 / 编辑本地合并后的最终内容；提交后以新 revision 落地。'
                : '直接编辑定稿内容；提交后以新 revision 落地。'
            }}
            内容不能为空。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-1.5">
          <Label for="resolve-content">最终内容（UTF-8 Markdown）</Label>
          <Textarea
            id="resolve-content"
            v-model="resolveDraft"
            class="min-h-48 font-mono text-xs"
            placeholder="合并 / 定稿后的完整文档内容…"
          />
          <p v-if="resolveDraft && !draftValid" class="text-destructive text-xs">
            内容不能为空（空文档请改用删除流程）。
          </p>
        </div>
        <DialogFooter>
          <Button variant="ghost" :disabled="!!resolving" @click="resolveDialog = null">取消</Button>
          <Button
            :disabled="!draftValid || !!resolving"
            @click="doResolve(resolveDialog!.mode, resolveDraft)"
          >
            {{ resolving ? '提交中…' : '提交解决' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
