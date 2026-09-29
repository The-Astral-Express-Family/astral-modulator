<!-- 文档与记忆浏览（S7-5）：受管文档 manifest 列表 + 单篇内容查看。
     - 侧栏：DocFileTree 目录树（目录由 path 前缀客户端合成；memory/ 保留
       子空间 BrainIcon 标注；行右键菜单 + 原生拖拽移动），manifest 仍为
       path 升序游标分页，「加载更多」续拉；include_deleted 开关含 tombstone
       行（deleted=true 徽章）；「仅看记忆」过滤（路由 query ?memory=1 预置）；
       骨架 ⇄ 清单/内容经 LoadSwap 平滑交换，不跳动。
     - 内容：点行经 GET documents/{path} 展示全文（tombstone 也返回——
       deleted=true + 最后一次内容，同步端据此侦测远端删除）。
     - 组织记忆入口：org-memory 是保留 workspace（注册时种子），经
       GET /workspaces?name=org-memory（契约：name 精确 name/slug 解析）定位；
       命中且非当前 workspace → 提供跳转入口；当前即 org-memory → 顶部徽章；
       不可见（未加入 / 服务端未铺）→ 说明文案（见下方 ORG_MEMORY_NOTE）。
     - R4 文档活动栏：SSE document.updated / document.conflict 事件流小面板
       （最近 20 条：path+revision+deleted / conflict_id），点击可跳详情或
       冲突视图；document.updated 同时防抖静默刷新清单与已选文档。
     - 内容渲染：默认走 MarkdownDoc 渲染视图（marked 解析 + DOMPurify 清洗，
       可切回源码、偏好 localStorage 记忆），见 components/shared/MarkdownDoc。
     - 编辑（会话级状态）：详情卡「编辑」进入 DocEditPanel（CM6 编辑、预览、
       保存带乐观并发 base；409 冲突引导去 ConflictsView）；清单头「新建文档」
       （path 预校验，push base_revision=0 create 分支）；tombstone 详情「恢复」
       走 revive 分支。编辑器与解析器均惰性 chunk，查看态零成本。
     - 管理（契约无 rename/move 端点，全部客户端组合既有端点）：
       删除 = GET 取最新 revision → DELETE?base_revision=（防 409 冲突工件；
       已是 tombstone 则跳过）；重命名/移动 = GET 旧文 → PUT 新路径
       （create 分支；目标为 tombstone 时即 revive）→ DELETE 旧路径
       （tombstone），两步间失败不回滚（新路径已落地，旧文仍在，可重试删旧）；
       目录操作 = 对子树逐文件执行（目录是客户端合成的，服务端无实体）。
       manifest 分页只加载首页时，目标冲突校验仅覆盖已加载行，服务端仍兜底。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { Activity, ArrowRight, Brain, FilePlusIcon, FolderInputIcon, FolderOpenIcon, PencilIcon, PenLineIcon, Trash2Icon } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { deleteDocument, getDocument, getDocumentManifest, pushDocument } from '@/api/modules/documents'
import type { DocumentDto, ManifestItemDto } from '@/api/modules/documents'
import { listWorkspaces } from '@/api/modules/core'
import type { Workspace } from '@/api/types'
import { useCursorList } from '@/composables/useCursorList'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useEventStream } from '@/composables/useEventStream'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import { fmtTime } from '@/lib/format'
import { SSE_VARIANTS } from '@/lib/sse'
import PageHeader from '@/components/shared/PageHeader.vue'
import MarkdownDoc from '@/components/shared/MarkdownDoc.vue'
import DocEditPanel from '@/components/shared/DocEditPanel.vue'
import DocFileTree from '@/components/documents/DocFileTree.vue'
import LoadSwap from '@/components/shared/LoadSwap.vue'
import { formatApiError } from '@/api/client'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

const workspaceId = useWorkspaceId()
const route = useRoute()
const router = useRouter()

const PAGE_LIMIT = 200
const MAX_ACTIVITY = 20

// ---- org-memory workspace 入口（契约：?name= 精确 name/slug 解析）----

const ORG_MEMORY_SLUG = 'org-memory'
const ORG_MEMORY_NOTE =
  '组织记忆工作区（org-memory）对当前账号不可见——可能尚未加入，或本服务未启用。可经左上角 workspace 切换器手动切换确认。'

const orgMemory = ref<Workspace | null>(null)
const orgMemoryLoaded = ref(false)

async function loadOrgMemory(): Promise<void> {
  try {
    // 失败不阻塞文档浏览；core.listWorkspaces 无 silent 尾参，错误由全局
    // toast 呈现（此时 manifest 请求通常同样失败，提示语义不冲突）。
    const page = await listWorkspaces({ name: ORG_MEMORY_SLUG })
    orgMemory.value = page.items.find((w) => w.slug === ORG_MEMORY_SLUG) ?? page.items[0] ?? null
  } catch {
    orgMemory.value = null
  } finally {
    orgMemoryLoaded.value = true
  }
}

const isOrgMemoryHere = computed(() => orgMemory.value?.id === workspaceId.value)
const orgMemoryLink = computed(() =>
  orgMemory.value && !isOrgMemoryHere.value ? `/workspaces/${orgMemory.value.id}/documents` : '',
)

// ---- 清单（path 升序游标 + include_deleted 开关 + 仅看记忆过滤）----

const includeDeleted = ref(false)
const memoryOnly = ref(route.query.memory === '1')

const isMemory = (path: string): boolean => path.startsWith('memory/')

// 清单分页：失败由全局拦截器 toast（silent 时为 SSE 防抖刷新，不打扰）。
const {
  items,
  cursor: nextCursor,
  loaded,
  loading,
  load: loadManifest,
  loadMore,
  reset: resetManifest,
} = useCursorList<ManifestItemDto>((cursor, opts) =>
  getDocumentManifest(
    workspaceId.value,
    {
      limit: PAGE_LIMIT,
      include_deleted: includeDeleted.value,
      ...(cursor ? { cursor } : {}),
    },
    { silent: opts.silent },
  ),
)

const visibleItems = computed(() =>
  memoryOnly.value ? items.value.filter((it) => isMemory(it.path)) : items.value,
)

function toggleIncludeDeleted(): void {
  includeDeleted.value = !includeDeleted.value
  nextCursor.value = null // 切换后旧游标失效；失败时不残留「加载更多」
  void loadManifest()
}

// ---- 内容查看（GET documents/{path}；tombstone 显示已删除态）----

const selectedPath = ref('')
const doc = ref<DocumentDto | null>(null)
const docLoading = ref(false)

function selectPath(path: string): void {
  if (docLoading.value && selectedPath.value === path) return
  selectedPath.value = path
  doc.value = null
  void loadDoc()
}

async function loadDoc(opts: { silent?: boolean } = {}): Promise<void> {
  if (!selectedPath.value) return
  docLoading.value = true
  try {
    doc.value = await getDocument(workspaceId.value, selectedPath.value, { silent: opts.silent })
    tryPendingEdit()
  } catch {
    // 404/403 已由全局拦截器 toast；保留选中路径，内容区显示空态。
    doc.value = null
    pendingEdit.value = null
  } finally {
    docLoading.value = false
  }
}

// ---- 编辑态（会话级，不进路由；编辑器惰性 chunk）----

const editing = ref(false)
const editDirty = ref(false)

// 路由离开守卫：脏编辑时确认，防止误触导航丢稿。
onBeforeRouteLeave(() => {
  if (editing.value && editDirty.value && !window.confirm('文档尚未保存，确定离开？'))
    return false
  return true
})

function startEdit(): void {
  if (!doc.value) return
  // tombstone 也允许进入编辑态：保存即走 revive 分支（base_revision=0）。
  editing.value = true
}

// 右键菜单「编辑 / 恢复」的意图：先选中（触发加载），内容就位后自动进入
// 编辑态；tombstone 只响应 restore（revive 语义由 DocEditPanel revive 承接）。
const pendingEdit = ref<'edit' | 'restore' | null>(null)

function openAndEdit(path: string, mode: 'edit' | 'restore'): void {
  pendingEdit.value = mode
  if (selectedPath.value === path) void tryPendingEdit()
  else selectPath(path)
}

function tryPendingEdit(): void {
  const want = pendingEdit.value
  if (!want || !doc.value) return
  const deleted = doc.value.deleted === true
  if ((want === 'edit' && !deleted) || (want === 'restore' && deleted)) {
    editing.value = true
    pendingEdit.value = null
  }
}

function stopEdit(): void {
  editing.value = false
  pendingEdit.value = null
}

function onSaved(d: DocumentDto): void {
  editing.value = false
  doc.value = d
  void loadManifest({ silent: true })
}

function goConflicts(): void {
  void router.push(`/workspaces/${workspaceId.value}/conflicts`)
}

// ---- 新建文档（清单头/目录右键入口，create 分支 base_revision=0）----

const createOpen = ref(false)
const newPath = ref('')
const newPathError = ref('')
const creating = ref(false)

const PATH_RE = /^[a-z0-9][a-z0-9._-]*(\/[a-z0-9][a-z0-9._-]*)*$/i

function openCreate(prefix = ''): void {
  newPath.value = prefix
  newPathError.value = ''
  createOpen.value = true
}

function validateNewPath(): boolean {
  const p = newPath.value.trim()
  if (!p) {
    newPathError.value = '请输入路径。'
    return false
  }
  if (!PATH_RE.test(p)) {
    newPathError.value = '路径仅限字母数字与 . _ - 与 /，且不含空段。'
    return false
  }
  if (p.includes('..') || p.startsWith('/')) {
    newPathError.value = '不允许 .. 或绝对路径。'
    return false
  }
  if (p.toLowerCase().startsWith('memory/')) {
    newPathError.value = 'memory/ 为保留子空间，组织记忆请经 CLI/agent 写入。'
    return false
  }
  // 对照全量已加载清单（而非当前过滤视图），避免过滤态下漏查重。
  if (items.value.some((it) => it.path === p)) {
    newPathError.value = '该路径已存在（可开启「含已删除」确认 tombstone）。'
    return false
  }
  newPathError.value = ''
  return true
}

async function createDoc(): Promise<void> {
  if (!validateNewPath() || creating.value) return
  creating.value = true
  try {
    const d = await pushDocument(
      workspaceId.value,
      newPath.value.trim(),
      { base_revision: 0, base_hash: `sha256:${'0'.repeat(64)}`, content: '' },
      { silent: true },
    )
    createOpen.value = false
    newPath.value = ''
    await refreshManifestAfterWrite()
    selectedPath.value = d.path
    doc.value = d
    editing.value = true
  } catch (e) {
    newPathError.value = formatApiError(e)
  } finally {
    creating.value = false
  }
}

// 写后刷新：清游标重拉首页，保持清单与新 revision 同步。
async function refreshManifestAfterWrite(): Promise<void> {
  nextCursor.value = null
  await loadManifest({ silent: true })
}

// ---- 管理操作：删除 / 重命名 / 移动 ----
// 契约无专用端点，全部客户端组合既有端点（详见文件头注释）。所有写调用
// silent：错误由本节就地/toast 呈现，避免全局拦截器双弹。

const ZERO_HASH = `sha256:${'0'.repeat(64)}`

// 取最新行：manifest 里的 revision 可能已过期，拿过期 base 去 DELETE 会
// 409 并产生冲突工件污染冲突列表。
async function fetchFresh(path: string): Promise<DocumentDto> {
  return getDocument(workspaceId.value, path, { silent: true })
}

// 单文件 tombstone（已是 tombstone 则跳过——DELETE 幂等，但没必要多打）。
async function deleteOne(path: string): Promise<void> {
  const d = await fetchFresh(path)
  if (d.deleted !== true) {
    await deleteDocument(workspaceId.value, path, d.revision, { silent: true })
  }
}

// 单文件移动/重命名：PUT 新路径（create 分支；目标是 tombstone 即 revive）
// → DELETE 旧路径。第二步失败不回滚：新文已落地、旧文仍在，toast 提示补删。
async function moveOne(fromPath: string, toPath: string): Promise<void> {
  const d = await fetchFresh(fromPath)
  await pushDocument(
    workspaceId.value,
    toPath,
    { base_revision: 0, base_hash: ZERO_HASH, content: d.content },
    { silent: true },
  )
  try {
    await deleteDocument(workspaceId.value, fromPath, d.revision, { silent: true })
  } catch {
    toast.warning(`已创建 ${toPath}，但旧路径删除失败`, {
      description: `${fromPath} 仍保留（可能被并发修改），可稍后在清单中手动删除。`,
    })
  }
}

// 选中项在移动后的重映射：文件换名直改，目录换前缀平移；内容重拉。
function selectionAfterMove(from: string, to: string, isDir: boolean): void {
  const hit =
    selectedPath.value === from ||
    (isDir && selectedPath.value.startsWith(`${from}/`))
  if (!hit) return
  selectedPath.value =
    selectedPath.value === from ? to : to + selectedPath.value.slice(from.length)
  doc.value = null
  editing.value = false
  void loadDoc({ silent: true })
}

// ---- 删除（对话框确认；目录为子树批量）----

const deleteOpen = ref(false)
const deleteTarget = ref('')
const deleteIsDir = ref(false)
const deleteRunning = ref(false)

const deleteDescendants = computed(() =>
  deleteIsDir.value
    ? items.value.filter((it) => !it.deleted && it.path.startsWith(`${deleteTarget.value}/`))
    : [],
)

function openDelete(path: string, isDir: boolean): void {
  deleteTarget.value = path
  deleteIsDir.value = isDir
  deleteRunning.value = false
  deleteOpen.value = true
}

async function runDelete(): Promise<void> {
  if (deleteRunning.value) return
  deleteRunning.value = true
  const target = deleteTarget.value
  const isDir = deleteIsDir.value
  try {
    if (!isDir) {
      await deleteOne(target)
      toast.success(`已删除 ${target}`)
    } else {
      let ok = 0
      const failures: string[] = []
      for (const it of deleteDescendants.value) {
        try {
          await deleteOne(it.path)
          ok++
        } catch {
          failures.push(it.path)
        }
      }
      if (!failures.length) toast.success(`已删除目录 ${target}（${ok} 项）`)
      else
        toast.warning(`目录删除完成 ${ok}/${deleteDescendants.value.length}`, {
          description: `失败：${failures.slice(0, 3).join('、')}${failures.length > 3 ? ' 等' : ''}`,
        })
    }
    if (
      selectedPath.value === target ||
      (isDir && selectedPath.value.startsWith(`${target}/`))
    ) {
      selectedPath.value = ''
      doc.value = null
      editing.value = false
    }
    deleteOpen.value = false
    await refreshManifestAfterWrite()
  } catch (e) {
    const msg = formatApiError(e)
    if (/DOCUMENT_CONFLICT/.test(msg)) {
      // base 失配：服务端已落冲突工件，引导去裁决。
      deleteOpen.value = false
      toast.error('删除冲突：远端已有变更', {
        description: `${msg} 可前往「冲突」页裁决。`,
      })
    } else {
      toast.error(`删除失败：${msg}`)
    }
  } finally {
    deleteRunning.value = false
  }
}

// ---- 重命名 / 移动（同一对话框，mode 只影响文案；目录为子树批量）----

const moveOpen = ref(false)
const moveMode = ref<'rename' | 'move'>('rename')
const moveSource = ref('')
const moveIsDir = ref(false)
const moveTarget = ref('')
const moveError = ref('')
const moveRunning = ref(false)

function openMove(path: string, isDir: boolean, mode: 'rename' | 'move', prefill?: string): void {
  moveMode.value = mode
  moveSource.value = path
  moveIsDir.value = isDir
  moveTarget.value = prefill ?? path
  moveError.value = ''
  moveRunning.value = false
  moveOpen.value = true
}

// 目录移动的落点冲突：子树内路径按前缀映射是单射，只会与子树外的现存行相撞
// （dstDir 在 srcDir 子树内的情形已先行拒绝）。
function dirMoveCollisions(srcDir: string, dstDir: string): string[] {
  const out: string[] = []
  for (const it of items.value) {
    if (it.deleted || !it.path.startsWith(`${srcDir}/`)) continue
    const np = dstDir + it.path.slice(srcDir.length)
    if (items.value.some((o) => !o.deleted && o.path === np)) out.push(np)
  }
  return out
}

function validateMoveTarget(): string {
  const s = moveSource.value
  const t = moveTarget.value.trim()
  if (!t) return '请输入目标路径。'
  if (t === s) return '目标路径与当前相同。'
  if (!PATH_RE.test(t)) return '路径仅限字母数字与 . _ - 与 /，且不含空段。'
  if (t.includes('..') || t.startsWith('/')) return '不允许 .. 或绝对路径。'
  if (moveIsDir.value) {
    if (t.startsWith(`${s}/`)) return '不能把目录移入其自身子树。'
    const collisions = dirMoveCollisions(s, t)
    if (collisions.length) {
      return `目标已存在：${collisions[0]}${collisions.length > 1 ? `（共 ${collisions.length} 项冲突）` : ''}`
    }
  } else if (items.value.some((it) => it.path === t && !it.deleted)) {
    return '目标路径已存在。'
  }
  return ''
}

async function runMove(): Promise<void> {
  if (moveRunning.value) return
  const err = validateMoveTarget()
  if (err) {
    moveError.value = err
    return
  }
  moveRunning.value = true
  moveError.value = ''
  const from = moveSource.value
  const to = moveTarget.value.trim()
  const verb = moveMode.value === 'rename' ? '重命名' : '移动'
  try {
    if (!moveIsDir.value) {
      await moveOne(from, to)
      toast.success(`已${verb}为 ${to}`)
    } else {
      const descendants = items.value.filter(
        (it) => !it.deleted && it.path.startsWith(`${from}/`),
      )
      let ok = 0
      const failures: string[] = []
      for (const it of descendants) {
        try {
          await moveOne(it.path, to + it.path.slice(from.length))
          ok++
        } catch {
          failures.push(it.path)
        }
      }
      if (!failures.length) toast.success(`目录已${verb}（${ok} 项）`)
      else
        toast.warning(`目录${verb}完成 ${ok}/${descendants.length}`, {
          description: `失败：${failures.slice(0, 3).join('、')}${failures.length > 3 ? ' 等' : ''}`,
        })
    }
    moveOpen.value = false
    selectionAfterMove(from, to, moveIsDir.value)
    await refreshManifestAfterWrite()
  } catch (e) {
    moveError.value = formatApiError(e)
  } finally {
    moveRunning.value = false
  }
}

// ---- 树拖放移动 ----
// 文件：直接执行（单文档、可再拖回，低风险）；目录：只预填移动对话框，
// 批量不可逆操作由用户在对话框里确认。

async function onTreeDrop(from: string, fromIsDir: boolean, toDir: string): Promise<void> {
  const name = from.split('/').pop() ?? from
  const to = toDir ? `${toDir}/${name}` : name
  if (fromIsDir) {
    openMove(from, true, 'move', to)
    return
  }
  if (items.value.some((it) => it.path === to && !it.deleted)) {
    toast.error(`目标已存在：${to}`, { description: '请改用「移动到…」对话框处理。' })
    return
  }
  try {
    await moveOne(from, to)
    toast.success(`已移动为 ${to}`)
    selectionAfterMove(from, to, false)
    await refreshManifestAfterWrite()
  } catch (e) {
    toast.error(`移动失败：${formatApiError(e)}`)
  }
}

// ---- R4 文档活动栏（document.updated / document.conflict）----

interface DocActivity {
  envId: string
  type: 'document.updated' | 'document.conflict'
  occurredAt: string
  path: string
  revision?: number
  deleted?: boolean
  conflictId?: string
}

const { state: sseState } = useEventStream(workspaceId, { onEvent: onSseEvent })
const activity = ref<DocActivity[]>([])

// 事件 data payload 的事实定义在服务端 document/push.go（emitUpdatedTx /
// recordConflictTx）：updated{path,revision,content_hash,deleted}、
// conflict{conflict_id,path,current_revision,current_hash}。按字段名防御式取值。
function str(data: Record<string, unknown>, key: string): string {
  const v = data[key]
  return typeof v === 'string' ? v : ''
}
function num(data: Record<string, unknown>, key: string): number | undefined {
  const v = data[key]
  return typeof v === 'number' ? v : undefined
}

let reloadTimer: ReturnType<typeof setTimeout> | null = null

function onSseEvent(env: { id: string; type: string; occurred_at: string; data: unknown }): void {
  if (env.type !== 'document.updated' && env.type !== 'document.conflict') return
  const data = (env.data && typeof env.data === 'object' ? env.data : {}) as Record<string, unknown>
  const entry: DocActivity = {
    envId: env.id,
    type: env.type,
    occurredAt: env.occurred_at,
    path: str(data, 'path'),
    revision: env.type === 'document.updated' ? num(data, 'revision') : num(data, 'current_revision'),
    deleted: env.type === 'document.updated' ? data.deleted === true : undefined,
    conflictId: env.type === 'document.conflict' ? str(data, 'conflict_id') : undefined,
  }
  activity.value = [entry, ...activity.value].slice(0, MAX_ACTIVITY)

  // document.updated 防抖静默刷新清单（游标重置回首页，MessagesView 同精神）；
  // 已选文档命中同一 path 时内容一并刷新。
  if (env.type === 'document.updated') {
    if (reloadTimer) clearTimeout(reloadTimer)
    const touched = entry.path
    reloadTimer = setTimeout(() => {
      reloadTimer = null
      void loadManifest({ silent: true })
      if (touched && touched === selectedPath.value) void loadDoc({ silent: true })
    }, 800)
  }
}

onUnmounted(() => {
  if (reloadTimer) clearTimeout(reloadTimer)
})

function openActivity(entry: DocActivity): void {
  if (entry.type === 'document.conflict' && entry.conflictId) {
    // 跳冲突视图（路由由主代理布线；query.conflict 为详情深链参数）。
    void router.push({
      path: `/workspaces/${workspaceId.value}/conflicts`,
      query: { conflict: entry.conflictId },
    })
    return
  }
  if (entry.path) selectPath(entry.path)
}

// ---- 展示辅助 ----

const fmtClock = (iso: string): string => new Date(iso).toLocaleTimeString()

// ---- 生命周期 ----

onMounted(() => {
  void loadManifest()
  void loadOrgMemory()
})

watch([workspaceId], () => {
  resetManifest()
  includeDeleted.value = false
  selectedPath.value = ''
  doc.value = null
  activity.value = []
  orgMemory.value = null
  orgMemoryLoaded.value = false
  void loadManifest()
  void loadOrgMemory()
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <PageHeader
        title="文档与记忆"
        description="受管 Markdown 的同步基准清单与内容查看；memory/ 前缀是组织记忆保留子空间。"
      />
      <div class="flex items-center gap-2">
        <Badge :variant="SSE_VARIANTS[sseState] ?? 'outline'">
          <Activity class="size-3" />
          SSE {{ sseState }}
        </Badge>
        <Button
          v-if="orgMemoryLink"
          variant="outline"
          size="sm"
          as-child
        >
          <RouterLink :to="orgMemoryLink">
            <Brain class="size-3.5" />
            组织记忆工作区
            <ArrowRight class="size-3.5" />
          </RouterLink>
        </Button>
        <Badge v-else-if="isOrgMemoryHere" variant="secondary">
          <Brain class="size-3" />
          组织记忆工作区
        </Badge>
      </div>
    </div>

    <p
      v-if="orgMemoryLoaded && !orgMemory"
      class="text-muted-foreground border-border rounded-lg border border-dashed px-3 py-2 text-xs"
    >
      {{ ORG_MEMORY_NOTE }}
    </p>

    <div class="grid gap-4 lg:grid-cols-12">
      <!-- 清单（目录树；lg 起 sticky + 内部滚动，长文阅读时侧栏常驻） -->
      <Card
        class="lg:col-span-4 lg:sticky lg:top-8 lg:max-h-[calc(100vh-4rem)] lg:self-start xl:col-span-3"
      >
        <CardHeader class="gap-1.5">
          <CardTitle class="flex items-center gap-2 text-base">
            <FolderOpenIcon class="size-4" />
            文档清单
          </CardTitle>
        </CardHeader>
        <CardContent class="flex min-h-0 flex-1 flex-col gap-2">
          <div class="flex flex-wrap items-center gap-2">
            <div class="border-input flex gap-0.5 rounded-lg border p-0.5">
              <Button
                :variant="includeDeleted ? 'default' : 'ghost'"
                size="xs"
                @click="toggleIncludeDeleted"
              >
                含已删除
              </Button>
              <Button
                :variant="memoryOnly ? 'default' : 'ghost'"
                size="xs"
                @click="memoryOnly = !memoryOnly"
              >
                <Brain class="size-3" />
                仅看记忆
              </Button>
            </div>
            <Button size="xs" variant="outline" class="ml-auto" @click="openCreate()">
              <FilePlusIcon class="size-3" />
              新建文档
            </Button>
          </div>

          <!-- 骨架 ⇄ 清单平滑交换：行高 h-7 贴近真实树行（~28px），5 行压低
               初始骨架总高，交换时高度经 LoadSwap 过渡不跳动。滚动区放在
               LoadSwap 根上（fr 行对 auto 容器按内容解析，滚动链不破）。 -->
          <LoadSwap
            class="min-h-0 flex-1 overflow-y-auto pr-0.5"
            :loading="loading && !items.length"
          >
            <template #skeleton>
              <div class="flex flex-col gap-2 p-1">
                <Skeleton v-for="i in 5" :key="i" class="h-7" :style="{ width: `${94 - i * 5}%` }" />
              </div>
            </template>
            <template #default>
              <DocFileTree
                v-if="visibleItems.length"
                :items="visibleItems"
                :selected-path="selectedPath"
                @select="selectPath"
                @edit="(p) => openAndEdit(p, 'edit')"
                @restore="(p) => openAndEdit(p, 'restore')"
                @rename="(p, isDir) => openMove(p, isDir, 'rename')"
                @move="(p, isDir) => openMove(p, isDir, 'move')"
                @remove="openDelete"
                @create-in="(d) => openCreate(d ? `${d}/` : '')"
                @move-drop="onTreeDrop"
              />
              <Empty v-else-if="loaded" class="border">
                <EmptyHeader>
                  <EmptyTitle>没有匹配的文档。</EmptyTitle>
                  <EmptyDescription>
                    {{
                      memoryOnly
                        ? '当前 workspace 还没有 memory/ 前缀的文档（组织记忆经 CLI 写入）。'
                        : includeDeleted
                          ? '清单为空：还没有受管文档，也没有 tombstone。'
                          : '清单为空：还没有受管文档。开启「含已删除」可查看 tombstone。'
                    }}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </template>
          </LoadSwap>

          <div v-if="visibleItems.length" class="flex items-center justify-between gap-2">
            <span class="text-muted-foreground text-xs">
              已加载 {{ visibleItems.length }} 项（path 升序）
            </span>
            <Button
              v-if="nextCursor"
              variant="outline"
              size="xs"
              :disabled="loading"
              @click="loadMore"
            >
              加载更多
            </Button>
          </div>
        </CardContent>
      </Card>

      <!-- 内容 -->
      <Card class="lg:col-span-8 xl:col-span-9">
        <CardHeader class="gap-1.5">
          <CardTitle class="flex min-w-0 items-center gap-2 text-base">
            <template v-if="selectedPath">
              <span class="truncate font-mono text-sm">{{ selectedPath }}</span>
              <Badge v-if="isMemory(selectedPath)" variant="secondary" class="shrink-0">
                <Brain class="size-3" />
                记忆
              </Badge>
              <Badge v-if="doc?.deleted" variant="destructive" class="shrink-0">已删除（tombstone）</Badge>
              <span class="grow" />
              <Button
                v-if="doc && !editing"
                size="xs"
                variant="outline"
                :disabled="docLoading"
                @click="startEdit"
              >
                <PencilIcon class="size-3" />
                {{ doc.deleted ? '恢复' : '编辑' }}
              </Button>
            </template>
            <template v-else>内容</template>
          </CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <template v-if="selectedPath">
            <LoadSwap :loading="docLoading && !doc">
              <template #skeleton>
                <div class="flex flex-col gap-2">
                  <Skeleton class="h-5 w-1/2" />
                  <Skeleton class="h-24 w-full" />
                  <Skeleton class="h-5 w-2/3" />
                </div>
              </template>
              <template #default>
                <template v-if="doc">
                  <template v-if="editing">
                    <DocEditPanel
                      :workspace-id="workspaceId"
                      :path="selectedPath"
                      :base="doc"
                      :revive="doc.deleted"
                      @saved="onSaved"
                      @cancel="stopEdit"
                      @conflict="goConflicts"
                      @dirty-change="editDirty = $event"
                    />
                  </template>
                  <template v-else>
                  <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
                    <span class="text-muted-foreground">revision <code class="font-mono">r{{ doc.revision }}</code></span>
                    <span class="text-muted-foreground font-mono" :title="doc.content_hash">
                      {{ doc.content_hash }}
                    </span>
                    <span class="text-muted-foreground">更新于 {{ fmtTime(doc.updated_at) }}</span>
                  </div>
                  <p v-if="doc.deleted" class="text-destructive rounded-lg border border-dashed px-3 py-2 text-xs">
                    该文档已打 tombstone（版本化删除）：行保留、revision 继续递增；对它的下
                    一次 push（base_revision=0）会将其复活。
                  </p>
                  <MarkdownDoc :content="doc.content" />
                  </template>
                </template>
                <Empty v-else class="border">
                  <EmptyHeader>
                    <EmptyTitle>内容不可见。</EmptyTitle>
                    <EmptyDescription>
                      读取失败（可能已无权限或路径已被清理），可重选左侧文档重试。
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </template>
            </LoadSwap>
          </template>
          <Empty v-else class="border">
            <EmptyHeader>
              <EmptyTitle>选择一篇文档。</EmptyTitle>
              <EmptyDescription>
                点左侧清单中的路径查看全文；memory/ 前缀条目需要 memory:read 权限。
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        </CardContent>
      </Card>
    </div>

    <!-- R4 文档活动栏 -->
    <Card>
      <CardHeader class="gap-1.5">
        <CardTitle class="flex items-center gap-2 text-base">
          <Activity class="size-4" />
          文档活动
          <span class="text-muted-foreground text-xs font-normal">
            document.updated / document.conflict 实时事件（最近 {{ MAX_ACTIVITY }} 条）
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-1.5">
        <template v-if="activity.length">
          <button
            v-for="a in activity"
            :key="a.envId"
            type="button"
            class="hover:bg-muted/40 flex flex-wrap items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs"
            @click="openActivity(a)"
          >
            <Badge :variant="a.type === 'document.conflict' ? 'destructive' : 'secondary'">
              {{ a.type === 'document.conflict' ? '冲突' : '更新' }}
            </Badge>
            <span class="min-w-0 flex-1 truncate font-mono">{{ a.path }}</span>
            <Badge v-if="a.deleted" variant="destructive">deleted</Badge>
            <span v-if="a.revision !== undefined" class="text-muted-foreground font-mono">
              r{{ a.revision }}
            </span>
            <span v-if="a.conflictId" class="text-muted-foreground hidden font-mono md:inline">
              {{ a.conflictId.slice(0, 12) }}…
            </span>
            <span class="text-muted-foreground">{{ fmtClock(a.occurredAt) }}</span>
          </button>
        </template>
        <p v-else class="text-muted-foreground px-2 py-3 text-xs">
          暂无文档事件：本 workspace 的文档写入 / 冲突产生后会实时出现在这里（SSE
          {{ sseState }}）。
        </p>
      </CardContent>
    </Card>
  </div>

  <!-- 新建文档：path 客户端预校验 + push create 分支（base_revision=0）。
       错误就地展示（silent 调用），成功后关对话框、刷新清单、直达编辑态。 -->
  <Dialog :open="createOpen" @update:open="createOpen = $event">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>新建文档</DialogTitle>
        <DialogDescription>
          输入仓库相对路径（如 docs/plan.md）；创建后直接进入编辑。
        </DialogDescription>
      </DialogHeader>
      <div class="flex flex-col gap-2">
        <Input
          v-model="newPath"
          placeholder="docs/plan.md"
          class="font-mono"
          :disabled="creating"
          @keyup.enter="createDoc"
        />
        <p v-if="newPathError" class="text-destructive text-xs">{{ newPathError }}</p>
      </div>
      <DialogFooter>
        <Button variant="outline" size="sm" :disabled="creating" @click="createOpen = false">
          取消
        </Button>
        <Button size="sm" :disabled="creating" @click="createDoc">
          {{ creating ? '创建中…' : '创建并编辑' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- 删除确认（版本化 tombstone，非物理删除；目录为子树批量）。
       base_revision 以 GET 最新行为准，避免过期 base 打出 409 冲突工件。 -->
  <Dialog :open="deleteOpen" @update:open="deleteOpen = $event">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <Trash2Icon class="text-destructive size-4" />
          {{ deleteIsDir ? '删除目录' : '删除文档' }}
        </DialogTitle>
        <DialogDescription>
          <template v-if="deleteIsDir">
            将对 <code class="font-mono">{{ deleteTarget }}/</code> 下
            {{ deleteDescendants.length }} 篇文档逐篇执行版本化删除（tombstone）。
          </template>
          <template v-else>
            将版本化删除 <code class="font-mono">{{ deleteTarget }}</code
            >：行保留、revision 续增，之后 push（base_revision=0）可复活。
          </template>
          若它正被其他端引用，请先确认再删除。
        </DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <Button variant="outline" size="sm" :disabled="deleteRunning" @click="deleteOpen = false">
          取消
        </Button>
        <Button variant="destructive" size="sm" :disabled="deleteRunning" @click="runDelete">
          {{
            deleteRunning
              ? '删除中…'
              : deleteIsDir
                ? `删除 ${deleteDescendants.length} 项`
                : '确认删除'
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <!-- 重命名 / 移动（同一对话框；目录为子树批量，目标 = 前缀替换）。
       服务端无 rename/move 端点：执行序 = PUT 新路径(create/revive) →
       DELETE 旧路径(tombstone)，语义见文件头注释与 runMove。 -->
  <Dialog :open="moveOpen" @update:open="moveOpen = $event">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <PenLineIcon v-if="moveMode === 'rename'" class="size-4" />
          <FolderInputIcon v-else class="size-4" />
          {{ moveIsDir ? `${moveMode === 'rename' ? '重命名' : '移动'}目录` : `${moveMode === 'rename' ? '重命名' : '移动'}文档` }}
        </DialogTitle>
        <DialogDescription>
          <template v-if="moveIsDir">
            目录 <code class="font-mono">{{ moveSource }}/</code> 下所有文档将按前缀映射迁到目标路径。
          </template>
          <template v-else>
            当前路径 <code class="font-mono">{{ moveSource }}</code
            >；历史版本随路径保留，新路径从 revision 1 重新计数。
          </template>
        </DialogDescription>
      </DialogHeader>
      <div class="flex flex-col gap-2">
        <label class="text-muted-foreground text-xs">目标路径（仓库相对路径）</label>
        <Input
          v-model="moveTarget"
          class="font-mono"
          :disabled="moveRunning"
          placeholder="docs/renamed.md"
          @keyup.enter="runMove"
        />
        <p v-if="moveError" class="text-destructive text-xs">{{ moveError }}</p>
      </div>
      <DialogFooter>
        <Button variant="outline" size="sm" :disabled="moveRunning" @click="moveOpen = false">
          取消
        </Button>
        <Button size="sm" :disabled="moveRunning" @click="runMove">
          {{ moveRunning ? (moveMode === 'rename' ? '重命名中…' : '移动中…') : '确认' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
