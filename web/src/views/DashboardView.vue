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
import { Skeleton } from '@/components/ui/skeleton'

const session = useSessionStore()
const { error: wsError, run } = useApiAction()
const workspaces = ref<Workspace[]>([])

onMounted(async () => {
  await session.boot()
  if (!session.isLoggedIn) return
  await run(async () => {
    workspaces.value = (await listWorkspaces()).items
  })
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="总览" />

    <!-- boot 未完成：Card 骨架占位。 -->
    <div v-if="!session.booted" class="flex flex-col gap-4">
      <Card v-for="i in 3" :key="i">
        <CardHeader>
          <Skeleton class="h-5 w-28" />
          <Skeleton class="h-4 w-48" />
        </CardHeader>
        <CardContent class="flex flex-col gap-2">
          <Skeleton class="h-4 w-full" />
          <Skeleton class="h-4 w-2/3" />
        </CardContent>
      </Card>
    </div>

    <div v-else class="flex flex-col gap-4">
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
  </div>
</template>
