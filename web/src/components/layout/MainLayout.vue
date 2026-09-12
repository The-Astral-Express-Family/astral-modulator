<!-- 主布局（'/' 父路由）：侧边栏 + 内容容器。/login 不经过此布局。 -->
<script setup lang="ts">
import { RouterView } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'
</script>

<template>
  <div class="flex min-h-screen">
    <AppSidebar />
    <main class="flex-1 p-6 lg:p-8">
      <div class="mx-auto flex w-full max-w-5xl flex-col gap-6">
        <RouterView />
      </div>
    </main>
  </div>
</template>
