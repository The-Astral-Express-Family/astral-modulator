<script setup lang="ts">
// 总览页：服务器发现信息 + 能力面板。这是与后端联调的第一屏。
import { onMounted, ref } from 'vue'
import { AstralApiError } from '../api/client'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const error = ref<string | null>(null)

onMounted(async () => {
  try {
    await session.loadServerInfo()
  } catch (e) {
    error.value = e instanceof AstralApiError ? `${e.code}: ${e.message}` : String(e)
  }
})
</script>

<template>
  <h2>总览</h2>

  <div v-if="error" class="card">
    <strong>无法连接 astral-server</strong>
    <p class="muted">{{ error }}</p>
    <p class="muted">请确认 server 已在 :8080 监听（make serve），或 Vite 代理配置正确。</p>
  </div>

  <div v-if="session.wellKnown" class="card">
    <h3>服务器</h3>
    <p>
      server_id: <code>{{ session.wellKnown.server_id }}</code>
      <span class="muted">（稳定身份，CLI 绑定主键）</span>
    </p>
    <p>canonical_url: {{ session.wellKnown.canonical_url }}</p>
    <p>protocol_version: {{ session.wellKnown.protocol_version }}</p>
  </div>

  <div v-if="session.capabilities" class="card">
    <h3>能力</h3>
    <p>minimum_cli_version: {{ session.capabilities.minimum_cli_version }}</p>
    <p>features:</p>
    <pre>{{ JSON.stringify(session.capabilities.features, null, 2) }}</pre>
    <p class="muted">
      features 为空表示脚手架阶段（业务端点均为 501 NOT_IMPLEMENTED 桩）。
    </p>
  </div>

  <!-- TODO(phase-2): workspace 列表与切换。 -->
  <!-- TODO(phase-3): 任务树视图 + 实时事件流（api/sse.ts 已就绪）。 -->
  <!-- TODO(phase-4): presence 总览。 -->
</template>
