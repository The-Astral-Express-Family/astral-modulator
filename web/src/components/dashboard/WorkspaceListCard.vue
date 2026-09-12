<script setup lang="ts">
// 我的 workspace 列表卡片：请求错误 / 列表 / 空态。
import type { Workspace } from '@/api/types'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { FolderOpenIcon } from '@lucide/vue'

defineProps<{
  workspaces: Workspace[]
  error: string | null
}>()
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>我的 Workspace</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <ErrorAlert v-if="error" :message="error" />
      <ul v-if="workspaces.length" class="flex flex-col gap-2">
        <li v-for="ws in workspaces" :key="ws.id" class="flex items-center gap-2">
          <Button variant="link" as-child>
            <RouterLink :to="`/workspaces/${ws.id}`">{{ ws.name }}</RouterLink>
          </Button>
          <span class="text-muted-foreground text-sm">（{{ ws.slug }}）</span>
        </li>
      </ul>
      <Empty v-else>
        <EmptyMedia variant="icon">
          <FolderOpenIcon />
        </EmptyMedia>
        <EmptyHeader>
          <EmptyTitle>还没有可访问的 workspace</EmptyTitle>
          <EmptyDescription>
            可先用 CLI：<code>astral init &lt;server&gt;</code>
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    </CardContent>
  </Card>
</template>
