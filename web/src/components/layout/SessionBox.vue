<!-- 侧边栏底部会话框：登录态展示 / 登出 / 登录入口。 -->
<script setup lang="ts">
import { RouterLink, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const router = useRouter()

async function logout(): Promise<void> {
  await session.logout()
  void router.push('/')
}
</script>

<template>
  <div class="flex flex-col gap-2 border-t px-4 py-4 text-sm">
    <template v-if="session.isLoggedIn">
      <p class="truncate text-muted-foreground">{{ session.actor?.display_name }}</p>
      <Button variant="ghost" size="sm" class="w-fit" @click="logout">登出</Button>
    </template>
    <template v-else-if="session.booted">
      <Button as-child>
        <RouterLink to="/login">登录</RouterLink>
      </Button>
    </template>
  </div>
</template>
