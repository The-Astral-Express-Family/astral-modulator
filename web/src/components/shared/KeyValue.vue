<!-- 键值对行：左标签右值，用于详情卡片的只读字段展示。 -->
<script setup lang="ts">
defineProps<{
  label: string
  value: string | number | null | undefined
}>()
</script>

<template>
  <div class="flex items-baseline justify-between gap-4">
    <span class="shrink-0 text-sm text-muted-foreground">{{ label }}</span>
    <code class="truncate text-sm">{{ value ?? '—' }}</code>
  </div>
</template>
