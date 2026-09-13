<!-- 侧栏导航项：图标 + 文案，当前路由精确命中时高亮（sidebar 语义 token）。 -->
<script setup lang="ts">
import type { Component } from 'vue'
import { RouterLink } from 'vue-router'

defineProps<{
  to: string
  label: string
  icon: Component
}>()
</script>

<template>
  <RouterLink
    :to="to"
    class="flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground [&.router-link-exact-active]:bg-sidebar-accent [&.router-link-exact-active]:text-sidebar-accent-foreground"
  >
    <component :is="icon" class="size-4 shrink-0 text-muted-foreground" />
    {{ label }}
  </RouterLink>
</template>
