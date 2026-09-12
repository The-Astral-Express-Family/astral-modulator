<!-- 全局壳：boot 错误横幅 + 路由出口 + 全局 Toast。
     布局由路由层承担（'/' → MainLayout，/login 独立页）；boot 由路由守卫触发。 -->
<script setup lang="ts">
import { RouterView } from 'vue-router'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import { Toaster } from '@/components/ui/sonner'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
</script>

<template>
  <div class="min-h-screen">
    <div v-if="session.bootError" class="p-6">
      <ErrorAlert
        title="无法连接 astral-server"
        :message="`${session.bootError}（请确认服务端已启动，且开发代理指向正确端口）`"
      />
    </div>
    <RouterView />
    <Toaster />
  </div>
</template>
