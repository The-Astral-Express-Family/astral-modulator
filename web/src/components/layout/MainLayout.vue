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
    <!-- 移动端顶栏（h-12）占位：侧栏在 <lg 收纳为抽屉，内容下移避免被遮挡。 -->
    <div class="h-12 lg:hidden" aria-hidden="true"></div>
    <main class="min-w-0 flex-1 p-4 lg:p-8">
      <div
        class="mx-auto flex w-full flex-col gap-6"
        :class="route.meta.wide ? 'max-w-7xl' : 'max-w-5xl'"
      >
        <RouterView />
      </div>
    </main>
  </div>
</template>
