<!-- Workspace 总览：工作区信息 + 实时事件流（presence / TODO 树 phase-3+ 逐步实装）。 -->
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { getWorkspace } from '@/api/modules/core'
import type { Workspace } from '@/api/types'
import JsonBlock from '@/components/shared/JsonBlock.vue'
import KeyValue from '@/components/shared/KeyValue.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { useEventStream } from '@/composables/useEventStream'
import type { SseState } from '@/composables/useEventStream'
import { useWorkspaceId } from '@/composables/useWorkspaceId'

// 路由参数保持响应式：/workspaces/a → /workspaces/b 组件复用时正确重载。
const workspaceId = useWorkspaceId()
const workspace = ref<Workspace | null>(null)
// 事件缓冲与 SSE 生命周期（订阅 / 重订 / 退订）由 useEventStream 托管。
const { events, state: sseState } = useEventStream(workspaceId)

const SSE_VARIANTS: Record<SseState, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}

// 路由守卫保证登录态后本页才可到达；401（会话中途失效）由全局出口跳登录。
async function load(): Promise<void> {
  try {
    workspace.value = await getWorkspace(workspaceId.value)
  } catch {
    workspace.value = null // 404 不存在 / 403 无权限；UI 显示占位
  }
}

onMounted(() => {
  void load()
})

watch(workspaceId, () => {
  void load()
})
</script>

<template>
  <PageHeader title="工作区总览" description="工作区基础信息与实时事件流。" />

  <Card>
    <CardHeader>
      <CardTitle>工作区信息</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <KeyValue label="ID" :value="workspaceId" />
      <KeyValue v-if="workspace" label="名称" :value="workspace.name" />
      <KeyValue v-if="workspace" label="Code" :value="workspace.slug" />
      <div class="flex items-center justify-between gap-4">
        <span class="text-sm text-muted-foreground">SSE 状态</span>
        <Badge :variant="SSE_VARIANTS[sseState]">{{ sseState }}</Badge>
      </div>
      <Button as-child class="w-fit">
        <RouterLink :to="`/workspaces/${workspaceId}/approvals`">裁决队列</RouterLink>
      </Button>
    </CardContent>
  </Card>

  <Empty v-if="!workspace">
    <EmptyHeader>
      <EmptyTitle>（无权访问或不存在）</EmptyTitle>
      <EmptyDescription>当前工作区不存在，或登录态不足以访问。</EmptyDescription>
    </EmptyHeader>
  </Empty>

  <Card>
    <CardHeader>
      <CardTitle>实时事件</CardTitle>
    </CardHeader>
    <CardContent>
      <JsonBlock v-if="events.length > 0" :value="events" />
      <Empty v-else>
        <EmptyHeader>
          <EmptyTitle>（等待事件…）</EmptyTitle>
          <EmptyDescription>订阅已建立，事件到达后展示在此（最多保留 50 条）。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    </CardContent>
  </Card>

  <!-- TODO(phase-3): TODO 树视图（搜索框支持 regex+fuzzy 双输入，对应 CLI --regex/--fuzzy）。 -->
  <!-- TODO(phase-4): presence 总览、消息流。 -->
  <!-- TODO(phase-5): 文档活动与冲突解决。 -->
</template>
