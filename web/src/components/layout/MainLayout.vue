<!-- 主布局（'/' 父路由）：侧边栏 + 内容容器。/login 不经过此布局。
     meta.wide 的路由（如任务树双栏视图）内容列放宽到 max-w-7xl。 -->
<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'

const route = useRoute()
</script>

<template>
  <div class="flex min-h-screen">
    <AppSidebar />
    <main class="flex-1 p-6 lg:p-8">
      <div
        class="mx-auto flex w-full flex-col gap-6"
        :class="route.meta.wide ? 'max-w-7xl' : 'max-w-5xl'"
      >
        <RouterView />
      </div>
    </main>
  </div>
</template>
