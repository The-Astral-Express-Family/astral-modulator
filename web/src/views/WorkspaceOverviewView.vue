<script setup lang="ts">
// Workspace 总览：详情与实时事件流（presence/消息/文档视图见 TODO.md phase-4/5）。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { getWorkspace } from '../api/modules/core'
import { useWorkspaceEvents } from '../composables/useWorkspaceEvents'
import { useSessionStore } from '../stores/session'
import type { EventEnvelope, Workspace } from '../api/types'

const route = useRoute()
const session = useSessionStore()
// 路由参数保持响应式：/workspaces/a → /workspaces/b 组件复用时正确重载。
const workspaceId = computed(() => route.params.workspaceId as string)
const workspace = ref<Workspace | null>(null)
const events = ref<EventEnvelope[]>([])

const { sseState, subscribe } = useWorkspaceEvents(workspaceId, (env) => {
  events.value.unshift(env)
  if (events.value.length > 50) events.value.pop()
})

async function load(): Promise<void> {
  await session.boot()
  try {
    workspace.value = await getWorkspace(workspaceId.value)
  } catch {
    workspace.value = null // 401 未登录 / 404 无权限；UI 显示占位
  }
  subscribe()
}

onMounted(load)
watch(workspaceId, () => {
  events.value = []
  void load()
})
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
      · <RouterLink :to="`/workspaces/${workspaceId}/tasks`">任务树</RouterLink>
      · <RouterLink :to="`/workspaces/${workspaceId}/approvals`">裁决队列</RouterLink>
    </p>
  </div>
  <div class="card">
    <h3>实时事件</h3>
    <pre>{{ events.length ? JSON.stringify(events.slice(0, 10), null, 2) : '（等待事件…）' }}</pre>
  </div>
  <!-- TODO(phase-4): presence 总览、消息流。 -->
  <!-- TODO(phase-5): 文档活动与冲突解决。 -->
</template>
