<!-- 全局侧边栏：品牌 + workspace 上下文导航（切换器 + 子导航）+ 全局导航 +
     底部工具（设备授权）与会话框。配色走 sidebar 语义 token。
     演示身份（mock 模式）不消费真实 API，workspace 段整体隐藏。 -->
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { GavelIcon, HomeIcon, KeyRoundIcon, ListTreeIcon, UsersRoundIcon } from '@lucide/vue'
import SessionBox from '@/components/layout/SessionBox.vue'
import SidebarLink from '@/components/layout/SidebarLink.vue'
import WorkspaceSwitcher from '@/components/layout/WorkspaceSwitcher.vue'
import { useSessionStore } from '@/stores/session'

const route = useRoute()
const session = useSessionStore()

const workspaceId = computed(() =>
  typeof route.params.workspaceId === 'string' ? route.params.workspaceId : '',
)
</script>

<template>
  <aside class="flex w-56 shrink-0 flex-col border-r bg-sidebar text-sidebar-foreground">
    <div class="px-4 py-5">
      <h1 class="text-lg font-semibold">Astral</h1>
    </div>

    <nav class="flex flex-col gap-4 px-2">
      <div
        v-if="session.isLoggedIn && !session.isDemo"
        class="flex flex-col gap-1"
      >
        <p class="px-3 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
          工作区
        </p>
        <WorkspaceSwitcher />
        <div v-if="workspaceId" class="mt-1 flex flex-col gap-1">
          <SidebarLink label="概览" :icon="HomeIcon" :to="`/workspaces/${workspaceId}`" />
          <SidebarLink
            label="任务树"
            :icon="ListTreeIcon"
            :to="`/workspaces/${workspaceId}/tasks`"
          />
          <SidebarLink
            label="裁决队列"
            :icon="GavelIcon"
            :to="`/workspaces/${workspaceId}/approvals`"
          />
        </div>
        <p v-else class="px-3 text-xs text-muted-foreground">选择工作区查看其页面。</p>
      </div>

      <div class="flex flex-col gap-1">
        <SidebarLink label="总览" :icon="HomeIcon" to="/" />
      </div>

      <!-- 平台管理：仅平台管理员可见（展示性判断，服务端 RequireGlobal 强制为准；
           页面本身对 403 也是 fail-closed）。 -->
      <div v-if="session.isPlatformAdmin && !session.isDemo" class="flex flex-col gap-1">
        <p class="px-3 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
          平台管理
        </p>
        <SidebarLink label="用户管理" :icon="UsersRoundIcon" to="/admin/users" />
      </div>
    </nav>

    <div class="mt-auto flex flex-col">
      <div class="px-2 pb-2">
        <SidebarLink label="设备授权" :icon="KeyRoundIcon" to="/device" />
      </div>
      <SessionBox />
    </div>
  </aside>
</template>
