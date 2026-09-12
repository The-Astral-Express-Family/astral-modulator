<!-- 全局侧边栏：品牌 + 一级导航 + 底部会话框。配色走 sidebar 语义 token。 -->
<script setup lang="ts">
import { RouterLink } from 'vue-router'
import SessionBox from '@/components/layout/SessionBox.vue'

interface NavItem {
  to: string
  label: string
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', label: '总览' },
  { to: '/device', label: '设备授权' },
]
</script>

<template>
  <aside class="flex w-56 shrink-0 flex-col border-r bg-sidebar text-sidebar-foreground">
    <div class="px-4 py-5">
      <h1 class="text-lg font-semibold">Astral</h1>
    </div>
    <nav class="flex flex-col gap-1 px-2">
      <RouterLink
        v-for="item in NAV_ITEMS"
        :key="item.to"
        :to="item.to"
        class="rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground [&.router-link-active]:bg-sidebar-accent [&.router-link-active]:text-sidebar-accent-foreground"
      >
        {{ item.label }}
      </RouterLink>
    </nav>
    <SessionBox class="mt-auto" />
  </aside>
</template>
