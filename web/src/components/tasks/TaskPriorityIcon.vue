<!-- 优先级旗标：urgent/high/low 显示彩色旗，normal 不显示（默认态不占视觉）。 -->
<script setup lang="ts">
import { Flag } from '@lucide/vue'
import type { TaskPriority } from '@/api/types'
import { TASK_PRIORITY_META } from './taskMeta'

const props = defineProps<{ priority: TaskPriority }>()

const meta = () => TASK_PRIORITY_META[props.priority] ?? TASK_PRIORITY_META.normal
</script>

<template>
  <Flag
    v-if="meta().icon"
    class="size-3.5 shrink-0"
    :class="meta().icon"
    aria-label="priority"
  />
</template>
