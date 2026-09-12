<!-- 页头：标题 + 可选 muted 描述。 -->
<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()
</script>

<template>
  <header class="flex flex-col gap-1">
    <h2 class="text-2xl font-semibold tracking-tight">{{ title }}</h2>
    <p v-if="description" class="text-sm text-muted-foreground">{{ description }}</p>
  </header>
</template>
