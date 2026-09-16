<!-- 文档与记忆浏览（S7-5）：受管文档 manifest 列表 + 单篇内容查看。
     - 列表：path 升序游标分页（next_cursor = 末行 path），「加载更多」续拉；
       include_deleted 开关含 tombstone 行（deleted=true 徽章）；memory/ 前缀
       是保留子空间（M1 裁决），行内「记忆」徽章显著标注 + 「仅看记忆」过滤
       （路由 query ?memory=1 预置该过滤）。
     - 内容：点行经 GET documents/{path} 展示全文（tombstone 也返回——
       deleted=true + 最后一次内容，同步端据此侦测远端删除）。
     - 组织记忆入口：org-memory 是保留 workspace（注册时种子），经
       GET /workspaces?name=org-memory（契约：name 精确 name/slug 解析）定位；
       命中且非当前 workspace → 提供跳转入口；当前即 org-memory → 顶部徽章；
       不可见（未加入 / 服务端未铺）→ 说明文案（见下方 ORG_MEMORY_NOTE）。
     - R4 文档活动栏：SSE document.updated / document.conflict 事件流小面板
       （最近 20 条：path+revision+deleted / conflict_id），点击可跳详情或
       冲突视图；document.updated 同时防抖静默刷新清单与已选文档。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Activity, ArrowRight, Brain, FileText } from '@lucide/vue'
import { getDocument, getDocumentManifest } from '@/api/modules/documents'
import type { DocumentDto, ManifestItemDto } from '@/api/modules/documents'
import { listWorkspaces } from '@/api/modules/core'
import type { Workspace } from '@/api/types'
import { useCursorList } from '@/composables/useCursorList'
import { useEventStream } from '@/composables/useEventStream'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import { fmtTime } from '@/lib/format'
import { SSE_VARIANTS } from '@/lib/sse'
import PageHeader from '@/components/shared/PageHeader.vue'
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
  } catch {
    // 404/403 已由全局拦截器 toast；保留选中路径，内容区显示空态。
    doc.value = null
  } finally {
    docLoading.value = false
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
const shortHash = (hash: string): string => (hash ? `${hash.slice(0, 14)}…` : '—')
const fmtSize = (bytes: number): string =>
  bytes >= 1024 ? `${(bytes / 1024).toFixed(1)} KiB` : `${bytes} B`

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
      <!-- 清单 -->
      <Card class="lg:col-span-5">
        <CardHeader class="gap-1.5">
          <CardTitle class="flex items-center gap-2 text-base">
            <FileText class="size-4" />
            文档清单
          </CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-2">
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
            <span class="text-muted-foreground ml-auto text-xs">
              已加载 {{ visibleItems.length }} 项（path 升序）
            </span>
          </div>

          <div v-if="loading && !items.length" class="flex flex-col gap-2">
            <Skeleton v-for="i in 6" :key="i" class="h-9" :style="{ width: `${94 - i * 5}%` }" />
          </div>

          <template v-else-if="visibleItems.length">
            <button
              v-for="it in visibleItems"
              :key="it.path"
              type="button"
              class="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm"
              :class="it.path === selectedPath ? 'bg-primary/10' : 'hover:bg-muted/40'"
              @click="selectPath(it.path)"
            >
              <span class="min-w-0 flex-1 truncate font-mono text-xs">{{ it.path }}</span>
              <Badge v-if="isMemory(it.path)" variant="secondary" class="shrink-0">
                <Brain class="size-3" />
                记忆
              </Badge>
              <Badge v-if="it.deleted" variant="destructive" class="shrink-0">已删除</Badge>
              <span class="text-muted-foreground hidden shrink-0 font-mono text-xs sm:inline">
                r{{ it.revision }}
              </span>
              <span class="text-muted-foreground hidden shrink-0 font-mono text-xs lg:inline" :title="it.content_hash">
                {{ shortHash(it.content_hash) }}
              </span>
              <span class="text-muted-foreground hidden shrink-0 text-xs xl:inline">
                {{ fmtSize(it.size) }}
              </span>
            </button>
            <Button
              v-if="nextCursor"
              variant="outline"
              size="sm"
              class="self-start"
              :disabled="loading"
              @click="loadMore"
            >
              加载更多
            </Button>
          </template>

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
        </CardContent>
      </Card>

      <!-- 内容 -->
      <Card class="lg:col-span-7">
        <CardHeader class="gap-1.5">
          <CardTitle class="flex min-w-0 items-center gap-2 text-base">
            <template v-if="selectedPath">
              <span class="truncate font-mono text-sm">{{ selectedPath }}</span>
              <Badge v-if="isMemory(selectedPath)" variant="secondary" class="shrink-0">
                <Brain class="size-3" />
                记忆
              </Badge>
              <Badge v-if="doc?.deleted" variant="destructive" class="shrink-0">已删除（tombstone）</Badge>
            </template>
            <template v-else>内容</template>
          </CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <template v-if="selectedPath">
            <div v-if="docLoading && !doc" class="flex flex-col gap-2">
              <Skeleton class="h-5 w-1/2" />
              <Skeleton class="h-24 w-full" />
              <Skeleton class="h-5 w-2/3" />
            </div>
            <template v-else-if="doc">
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
              <pre class="bg-muted/40 max-h-[28rem] overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap break-all">{{ doc.content }}</pre>
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
</template>
