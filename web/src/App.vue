<!-- 顶层布局：侧边栏 + 会话引导（boot）+ 全局 Toast。 -->
<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterView } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import { Toaster } from '@/components/ui/sonner'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()

onMounted(() => {
  void session.boot()
})
</script>

<template>
  <div class="flex min-h-screen">
    <AppSidebar />
    <main class="flex-1 p-6 lg:p-8">
      <div class="mx-auto flex w-full max-w-5xl flex-col gap-6">
        <ErrorAlert
          v-if="session.bootError"
          title="无法连接 astral-server"
          :message="`${session.bootError}（请确认服务端已启动，且开发代理指向正确端口）`"
        />
        <RouterView />
      </div>
    </main>
    <Toaster />
  </div>
</template>
