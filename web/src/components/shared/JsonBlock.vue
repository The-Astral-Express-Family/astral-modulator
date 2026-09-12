<!-- 只读 JSON 展示块：语义 token 配色、横向滚动，不写全局 pre 样式。 -->
<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  value: unknown
}>()

const text = computed(() => JSON.stringify(props.value, null, 2))
</script>

<template>
  <pre class="overflow-x-auto rounded-md border bg-muted p-4 text-sm">{{ text }}</pre>
</template>
