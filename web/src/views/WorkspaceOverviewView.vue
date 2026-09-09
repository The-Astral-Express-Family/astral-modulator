<script setup lang="ts">
// Workspace 总览：presence + 任务概览 + 实时事件（Phase 2-4 逐步实装）。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { getWorkspace } from '../api/modules/core'
import { subscribeEvents } from '../api/sse'
import { useSessionStore } from '../stores/session'
import type { EventEnvelope, Workspace } from '../api/types'

const route = useRoute()
const session = useSessionStore()
// 路由参数保持响应式：/workspaces/a → /workspaces/b 组件复用时正确重载。
const workspaceId = computed(() => route.params.workspaceId as string)
const workspace = ref<Workspace | null>(null)
const events = ref<EventEnvelope[]>([])
const sseState = ref<'connecting' | 'open' | 'closed'>('connecting')
let unsubscribe: (() => void) | null = null

async function load(): Promise<void> {
  await session.boot()
  try {
    workspace.value = await getWorkspace(workspaceId.value)
  } catch {
    workspace.value = null // 401 未登录 / 404 无权限；UI 显示占位
  }
  unsubscribe?.()
  unsubscribe = subscribeEvents({
    workspaceId: workspaceId.value,
    onEvent: (env) => {
      events.value.unshift(env)
      if (events.value.length > 50) events.value.pop()
    },
    onStateChange: (s) => (sseState.value = s),
  })
}

onMounted(load)
watch(workspaceId, () => {
  events.value = []
  void load()
})
onUnmounted(() => unsubscribe?.())
</script>

<template>
  <h2>Workspace</h2>
  <div class="card">
    <p>id: <code>{{ workspaceId }}</code></p>
    <p class="muted">
      {{ workspace ? workspace.name : '（无权访问或不存在）' }}
    </p>
    <p>
      SSE 状态：{{ sseState }}
      · <RouterLink :to="`/workspaces/${workspaceId}/approvals`">裁决队列</RouterLink>
    </p>
  </div>
  <div class="card">
    <h3>实时事件</h3>
    <pre>{{ events.length ? JSON.stringify(events.slice(0, 10), null, 2) : '（等待事件…）' }}</pre>
  </div>
  <!-- TODO(phase-3): TODO 树视图（搜索框支持 regex+fuzzy 双输入，对应 CLI --regex/--fuzzy）。 -->
  <!-- TODO(phase-4): presence 总览、消息流。 -->
  <!-- TODO(phase-5): 文档活动与冲突解决。 -->
</template>
