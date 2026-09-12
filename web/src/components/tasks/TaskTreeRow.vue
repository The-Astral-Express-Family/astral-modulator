<!-- 树行：深度缩进 + 折叠箭头（children_count>0 才显示）+ 状态/优先级/标签/负责人。
     整行点击选中，箭头只管折叠。v2 行内不带 lease（仅 get/claim 响应填充），租约
     倒计时在详情面板展示。 -->
<script setup lang="ts">
import { ChevronDown, ChevronRight } from '@lucide/vue'
import type { Actor, Task } from '@/api/types'
import UserAvatar from '@/components/shared/UserAvatar.vue'
import { Badge } from '@/components/ui/badge'
import TaskPriorityIcon from './TaskPriorityIcon.vue'
import TaskStatusBadge from './TaskStatusBadge.vue'

export interface TreeRow {
  task: Task
  depth: number
  hasChildren: boolean
  expanded: boolean
}

const props = defineProps<{
  row: TreeRow
  selected: boolean
  assignee: Actor | null
}>()

defineEmits<{ select: []; toggle: [] }>()

const muted = () =>
  props.row.task.status === 'done' || props.row.task.status === 'cancelled'
const visibleTags = () => props.row.task.tags.slice(0, 3)
const hiddenTagCount = () => Math.max(props.row.task.tags.length - 3, 0)
</script>

<template>
  <button
    type="button"
    class="flex w-full items-center gap-1.5 rounded-md px-1 py-1.5 text-left transition-colors hover:bg-muted/60"
    :class="selected ? 'bg-muted' : ''"
    :style="{ paddingLeft: `${row.depth * 18 + 6}px` }"
    @click="$emit('select')"
  >
    <span
      v-if="row.hasChildren"
      class="text-muted-foreground hover:text-foreground inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded hover:bg-muted"
      role="button"
      :aria-label="row.expanded ? '折叠' : '展开'"
      @click.stop="$emit('toggle')"
    >
      <ChevronDown v-if="row.expanded" class="size-4" />
      <ChevronRight v-else class="size-4" />
    </span>
    <span v-else class="inline-block size-5 shrink-0" />

    <TaskStatusBadge :status="row.task.status" show-label class="w-[62px] shrink-0" />
    <TaskPriorityIcon :priority="row.task.priority" />

    <span
      class="truncate text-sm"
      :class="muted() ? 'text-muted-foreground line-through' : ''"
      :title="row.task.title"
    >
      {{ row.task.title }}
    </span>

    <span
      v-if="row.hasChildren"
      class="text-muted-foreground shrink-0 text-xs"
    >
      {{ row.task.children_count }} 子任务
    </span>

    <Badge
      v-for="tag in visibleTags()"
      :key="tag.id"
      variant="outline"
      class="hidden shrink-0 xl:inline-flex"
    >
      {{ tag.name }}
    </Badge>
    <span v-if="hiddenTagCount() > 0" class="text-muted-foreground hidden shrink-0 text-xs xl:inline">
      +{{ hiddenTagCount() }}
    </span>

    <span class="flex-1" />
    <UserAvatar v-if="assignee" :actor="assignee" size="sm" />
  </button>
</template>
