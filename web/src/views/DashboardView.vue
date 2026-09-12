<script setup lang="ts">
// 总览页：服务器信息 + 登录态 + workspace 入口。
import { onMounted, ref } from 'vue'
import { listWorkspaces } from '@/api/modules/core'
import type { Workspace } from '@/api/types'
import { useSessionStore } from '@/stores/session'
import { useApiAction } from '@/composables/useApiAction'
import CapabilitiesCard from '@/components/dashboard/CapabilitiesCard.vue'
import ServerCard from '@/components/dashboard/ServerCard.vue'
import WorkspaceListCard from '@/components/dashboard/WorkspaceListCard.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

const session = useSessionStore()
const { error: wsError, run } = useApiAction()
const workspaces = ref<Workspace[]>([])

// 路由守卫已保证 boot 完成后再挂载；未登录时本页匿名可看（下方登录引导卡片）。
onMounted(async () => {
  if (!session.isLoggedIn) return
  await run(async () => {
    workspaces.value = (await listWorkspaces()).items
  })
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="总览" />

    <ServerCard v-if="session.wellKnown" :well-known="session.wellKnown" />

      <!-- 未登录：提示 + 登录入口。 -->
      <Card v-if="!session.isLoggedIn">
        <CardHeader>
          <CardTitle>未登录</CardTitle>
          <CardDescription>
            登录后可管理 workspace；CLI 用户通过 <code>astral login</code> 设备授权。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button as-child>
            <RouterLink to="/login">登录</RouterLink>
          </Button>
        </CardContent>
      </Card>
    <WorkspaceListCard v-else :workspaces="workspaces" :error="wsError" />

    <CapabilitiesCard v-if="session.capabilities" :capabilities="session.capabilities" />
  </div>
</template>
