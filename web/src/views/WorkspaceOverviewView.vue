<script setup lang="ts">
// Workspace 总览：presence + 任务概览 + 实时事件（Phase 2-4 逐步实装）。
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { getWorkspace } from '../api/modules/core'
import { subscribeEvents } from '../api/sse'
import type { EventEnvelope, Workspace } from '../api/types'

const route = useRoute()
const workspaceId = route.params.workspaceId as string
const workspace = ref<Workspace | null>(null)
const events = ref<EventEnvelope[]>([])
const sseState = ref<'connecting' | 'open' | 'closed'>('connecting')
let unsubscribe: (() => void) | null = null

onMounted(async () => {
  try {
    workspace.value = await getWorkspace(workspaceId)
  } catch {
    workspace.value = null // 桩阶段预期 501；UI 显示占位
  }
  unsubscribe = subscribeEvents({
    workspaceId,
    onEvent: (env) => {
      events.value.unshift(env)
      if (events.value.length > 50) events.value.pop()
    },
    onStateChange: (s) => (sseState.value = s),
  })
})

onUnmounted(() => unsubscribe?.())
</script>

<template>
  <h2>Workspace</h2>
  <div class="card">
    <p>id: <code>{{ workspaceId }}</code></p>
    <p class="muted">
      {{ workspace ? workspace.name : '详情端点尚未实现（桩阶段）' }}
    </p>
    <p>SSE 状态：{{ sseState }}（事件流端点已实装，事件产生于业务模块落地后）</p>
  </div>
  <div class="card">
    <h3>实时事件</h3>
    <pre>{{ events.length ? JSON.stringify(events.slice(0, 10), null, 2) : '（等待事件…）' }}</pre>
  </div>
  <!-- TODO(phase-3): TODO 树视图（搜索框支持 regex+fuzzy 双输入，对应 CLI --regex/--fuzzy）。 -->
  <!-- TODO(phase-4): presence 总览、消息流。 -->
  <!-- TODO(phase-5): 文档活动与冲突解决。 -->
</template>
