<!-- 任务状态徽章：彩色圆点 + 中文状态词（六态）。 -->
<script setup lang="ts">
import type { TaskStatus } from '@/api/types'
import { TASK_STATUS_META } from './taskMeta'

const props = withDefaults(defineProps<{ status: TaskStatus; showLabel?: boolean }>(), {
  showLabel: true,
})

const meta = () => TASK_STATUS_META[props.status] ?? TASK_STATUS_META.open
</script>

<template>
  <span class="inline-flex items-center gap-1.5 whitespace-nowrap text-xs text-muted-foreground">
    <span class="size-2 shrink-0 rounded-full" :class="meta().dot" />
    <template v-if="showLabel">{{ meta().label }}</template>
  </span>
</template>
