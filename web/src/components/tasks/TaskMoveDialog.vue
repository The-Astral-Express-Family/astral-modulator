<!-- 移动到…对话框：懒加载树选择器。目标 = 根层或某个任务，确认后走
     moveTasks（不带 position = 追加目标兄弟尾部，协议 2.2）。自身、已加载
     子孙与当前父容器不可选（成环/无操作由前端预拦；服务端仍兜底 400）。
     选择器只拉直接子层（与左树同口径），上限 200/层，不接「加载更多」。 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown, ChevronRight, FolderInput, ListTree } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { notifyApiError } from '@/api/client'
import { taskApi } from '@/api/taskSource'
import type { Task } from '@/api/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { TASKS_MOCK } from '@/lib/mockMode'

const props = defineProps<{
  open: boolean
  task: Task | null
  workspaceId: string
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  /** 移动成功，回传服务端返回的最新任务 */
  moved: [task: Task]
}>()

const loading = ref(false)
const roots = ref<Task[]>([])
const childrenMap = ref<Map<string, Task[]>>(new Map())
const expanded = ref<Set<string>>(new Set())
const expandingIds = ref<Set<string>>(new Set())
// undefined = 未选；null = 根层；id = 目标任务容器
const chosen = ref<string | null | undefined>(undefined)
const submitting = ref(false)

// 不可选：自身 + 已加载子孙（防成环）+ 当前父容器（无操作移动也会 bump revision）。
const forbidden = computed<Set<string>>(() => {
  const bad = new Set<string>()
  if (!props.task) return bad
  bad.add(props.task.id)
  const walk = (id: string): void => {
    for (const kid of childrenMap.value.get(id) ?? []) {
      bad.add(kid.id)
      walk(kid.id)
    }
  }
  walk(props.task.id)
  if (props.task.parent_id) bad.add(props.task.parent_id)
  return bad
})

interface PickerRow {
  task: Task
  depth: number
  expanded: boolean
  hasChildren: boolean
}

const rows = computed<PickerRow[]>(() => {
  const out: PickerRow[] = []
  const walk = (list: Task[], depth: number): void => {
    for (const t of list) {
      const isOpen = expanded.value.has(t.id)
      out.push({
        task: t,
        depth,
        expanded: isOpen,
        hasChildren: t.children_count > 0,
      })
      if (isOpen) walk(childrenMap.value.get(t.id) ?? [], depth + 1)
    }
  }
  walk(roots.value, 0)
  return out
})

const chosenTitle = computed(() => {
  if (chosen.value === undefined) return null
  if (chosen.value === null) return '根层（workspace 容器）'
  return `「${tasksById.value.get(chosen.value)?.title ?? chosen.value}」的子任务`
})

const tasksById = computed(() => {
  const m = new Map<string, Task>()
  for (const t of roots.value) m.set(t.id, t)
  for (const list of childrenMap.value.values()) for (const t of list) m.set(t.id, t)
  return m
})

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    chosen.value = undefined
    if (roots.value.length) return
    loading.value = true
    try {
      roots.value = (await taskApi.listWorkspaceChildren(props.workspaceId, { limit: 200 })).items
    } catch (e) {
      // 真实 API 失败已由拦截器 toast；mock 抛错不走 axios，手动出口兜底。
      if (TASKS_MOCK) notifyApiError(e)
    } finally {
      loading.value = false
    }
  },
)

async function toggle(node: PickerRow): Promise<void> {
  const id = node.task.id
  if (expanded.value.has(id)) {
    const next = new Set(expanded.value)
    next.delete(id)
    expanded.value = next
    return
  }
  expanded.value = new Set([...expanded.value, id])
  if (!childrenMap.value.has(id)) {
    expandingIds.value = new Set([...expandingIds.value, id])
    try {
      childrenMap.value.set(id, (await taskApi.listTaskChildren(id, { limit: 200 })).items)
    } catch (e) {
      if (TASKS_MOCK) notifyApiError(e)
      const next = new Set(expanded.value)
      next.delete(id)
      expanded.value = next
    } finally {
      const pending = new Set(expandingIds.value)
      pending.delete(id)
      expandingIds.value = pending
    }
  }
}

async function submit(): Promise<void> {
  if (!props.task || chosen.value === undefined || submitting.value) return
  submitting.value = true
  try {
    const out = await taskApi.moveTasks(props.workspaceId, [
      {
        task_id: props.task.id,
        parent_id: chosen.value,
        expected_revision: props.task.revision,
      },
    ])
    toast.success(chosen.value ? '任务已移动。' : '任务已移到根层。')
    emit('moved', out.items[0] ?? props.task)
    emit('update:open', false)
  } catch (e) {
    if (TASKS_MOCK) notifyApiError(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>移动任务</DialogTitle>
        <DialogDescription>
          选择「{{ task?.title }}」的新容器{{ chosenTitle ? `：${chosenTitle}` : '' }}。
          追加到目标子层末尾；如需精确定位，可直接在树中拖拽。
        </DialogDescription>
      </DialogHeader>

      <div class="max-h-80 min-h-40 overflow-y-auto rounded-lg border p-1.5">
        <Skeleton v-if="loading" class="h-8 w-full" />
        <template v-else>
          <!-- 根层选项 -->
          <button
            type="button"
            class="hover:bg-muted/60 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm"
            :class="chosen === null ? 'bg-muted' : ''"
            :disabled="task?.parent_id === null"
            @click="chosen = null"
          >
            <ListTree class="text-muted-foreground size-4 shrink-0" />
            <span class="min-w-0 flex-1 truncate">根层（workspace 容器）</span>
            <span v-if="task?.parent_id === null" class="text-muted-foreground text-xs">当前位置</span>
          </button>

          <button
            v-for="node in rows"
            :key="node.task.id"
            type="button"
            class="hover:bg-muted/60 flex w-full items-center gap-1 rounded-md py-1.5 pr-2 text-left text-sm disabled:cursor-not-allowed disabled:opacity-50"
            :class="chosen === node.task.id ? 'bg-muted' : ''"
            :style="{ paddingLeft: `${node.depth * 18 + 4}px` }"
            :disabled="forbidden.has(node.task.id)"
            @click="chosen = node.task.id"
          >
            <span
              v-if="node.hasChildren"
              class="text-muted-foreground hover:text-foreground inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded hover:bg-muted"
              role="button"
              :aria-label="node.expanded ? '折叠' : '展开'"
              @click.stop="toggle(node)"
            >
              <Spinner v-if="expandingIds.has(node.task.id)" class="size-3.5" />
              <ChevronDown v-else-if="node.expanded" class="size-4" />
              <ChevronRight v-else class="size-4" />
            </span>
            <span v-else class="inline-block size-5 shrink-0" />
            <span class="min-w-0 flex-1 truncate" :title="node.task.title">{{ node.task.title }}</span>
            <span v-if="forbidden.has(node.task.id) && node.task.id !== task?.id" class="text-muted-foreground text-xs">
              {{ node.task.id === task?.parent_id ? '当前位置' : '子树内' }}
            </span>
          </button>
        </template>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">取消</Button>
        <Button :disabled="chosen === undefined || submitting" @click="submit">
          <Spinner v-if="submitting" data-icon="inline-start" />
          <FolderInput v-else class="size-4" />
          移动
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
