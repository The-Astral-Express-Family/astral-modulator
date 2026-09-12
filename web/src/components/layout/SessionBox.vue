<!-- 侧边栏底部会话框：登录态为用户菜单（头像+用户名 → 资料/登出），
     未登录为登录入口。 -->
<script setup lang="ts">
import { ChevronsUpDownIcon, LogOutIcon, UserRoundIcon } from '@lucide/vue'
import { RouterLink, useRouter } from 'vue-router'
import UserAvatar from '@/components/shared/UserAvatar.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const router = useRouter()

async function logout(): Promise<void> {
  await session.logout()
  void router.push('/')
}
</script>

<template>
  <div class="border-t px-4 py-3 text-sm">
    <template v-if="session.isLoggedIn">
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button variant="ghost" class="h-auto w-full justify-start gap-2 px-2 py-1.5 font-normal">
            <UserAvatar :actor="session.actor!" size="sm" />
            <span class="truncate">{{ session.actor?.display_name }}</span>
            <ChevronsUpDownIcon class="ml-auto size-4 shrink-0 text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="start" class="w-56">
          <DropdownMenuLabel class="flex min-w-0 flex-col gap-0.5">
            <span class="truncate font-medium">{{ session.actor?.display_name }}</span>
            <span v-if="session.email" class="truncate text-xs font-normal text-muted-foreground">
              {{ session.email }}
            </span>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem @select="router.push('/profile')">
            <UserRoundIcon />
            个人资料
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem @select="logout">
            <LogOutIcon />
            登出
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </template>
    <template v-else-if="session.booted">
      <Button as-child class="w-full">
        <RouterLink to="/login">登录</RouterLink>
      </Button>
    </template>
  </div>
</template>
