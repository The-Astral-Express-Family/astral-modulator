<!-- 行级右键菜单（shadcn ContextMenu + reka-ui）：ContextMenuTrigger as-child
     包裹 slot 里的行组件，动作全部 emit 回视图层（API 调用与回写集中在视图）。
     树模式行与搜索结果行共用；搜索行 hasChildren 恒为 false（无展开态）。 -->
<script setup lang="ts">
import {
  CircleDot,
  Copy,
  Flag,
  FolderInput,
  ListTree,
  Play,
  Plus,
  Trash,
  Undo2,
} from '@lucide/vue'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import { toast } from 'vue-sonner'
import {
  TASK_PRIORITIES,
  TASK_PRIORITY_META,
  TASK_STATUSES,
  TASK_STATUS_META,
} from './taskMeta'
import type { Task, TaskPriority, TaskStatus } from '@/api/types'

const props = withDefaults(
  defineProps<{
    task: Task
    /** 行是否可展开（树模式的容器任务）；false 时隐藏展开/折叠项 */
    hasChildren?: boolean
    expanded?: boolean
  }>(),
  { hasChildren: false, expanded: false },
)

const emit = defineEmits<{
  'open-detail': []
  toggle: []
  'create-subtask': []
  status: [TaskStatus]
  priority: [TaskPriority]
  claim: []
  move: []
  /** 移除 = 软删除（status→cancelled）；已取消时为恢复（status→open） */
  remove: []
}>()

function copyId(): void {
  void navigator.clipboard
    .writeText(props.task.id)
    .then(() => toast.success('任务 ID 已复制。'))
}
</script>

<template>
  <ContextMenu>
    <ContextMenuTrigger as-child>
      <slot />
    </ContextMenuTrigger>
    <ContextMenuContent class="min-w-44">
      <ContextMenuItem @select="emit('open-detail')">
        <ListTree class="size-4" />
        打开详情
      </ContextMenuItem>
      <ContextMenuItem v-if="hasChildren" @select="emit('toggle')">
        <CircleDot class="size-4" />
        {{ expanded ? '折叠子任务' : '展开子任务' }}
      </ContextMenuItem>
      <ContextMenuItem @select="emit('create-subtask')">
        <Plus class="size-4" />
        新建子任务
      </ContextMenuItem>

      <ContextMenuSeparator />

      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <CircleDot class="size-4" />
          状态
        </ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup
            :model-value="task.status"
            @update:model-value="(v) => emit('status', v as TaskStatus)"
          >
            <ContextMenuRadioItem
              v-for="s in TASK_STATUSES"
              :key="s"
              :value="s"
              :disabled="s === task.status"
            >
              {{ TASK_STATUS_META[s].label }}
            </ContextMenuRadioItem>
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>

      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <Flag class="size-4" />
          优先级
        </ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup
            :model-value="task.priority"
            @update:model-value="(v) => emit('priority', v as TaskPriority)"
          >
            <ContextMenuRadioItem
              v-for="p in TASK_PRIORITIES"
              :key="p"
              :value="p"
              :disabled="p === task.priority"
            >
              {{ TASK_PRIORITY_META[p].label }}
            </ContextMenuRadioItem>
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>

      <ContextMenuItem @select="emit('claim')">
        <Play class="size-4" />
        认领（启动租约）
      </ContextMenuItem>
      <ContextMenuItem @select="emit('move')">
        <FolderInput class="size-4" />
        移动到…
      </ContextMenuItem>

      <ContextMenuSeparator />

      <ContextMenuItem
        :class="task.status === 'cancelled' ? '' : 'text-destructive focus:text-destructive'"
        @select="emit('remove')"
      >
        <template v-if="task.status === 'cancelled'">
          <Undo2 class="size-4" />
          恢复为待办
        </template>
        <template v-else>
          <Trash class="size-4" />
          移除（设为取消）
        </template>
      </ContextMenuItem>
      <ContextMenuItem @select="copyId()">
        <Copy class="size-4" />
        复制任务 ID
      </ContextMenuItem>
    </ContextMenuContent>
  </ContextMenu>
</template>
