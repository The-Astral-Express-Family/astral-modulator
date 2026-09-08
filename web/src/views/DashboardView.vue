<script setup lang="ts">
// 总览页：服务器信息 + 登录态 + workspace 入口。
import { onMounted, ref } from 'vue'
import { AstralApiError } from '../api/client'
import { listWorkspaces } from '../api/modules/core'
import { useSessionStore } from '../stores/session'
import type { Workspace } from '../api/types'

const session = useSessionStore()
const error = ref<string | null>(null)
const workspaces = ref<Workspace[]>([])

onMounted(async () => {
  await session.boot()
  if (!session.isLoggedIn) return
  try {
    workspaces.value = (await listWorkspaces()).items
  } catch (e) {
    error.value = e instanceof AstralApiError ? `${e.code}: ${e.message}` : String(e)
  }
})
</script>

<template>
  <h2>总览</h2>

  <div v-if="session.wellKnown" class="card">
    <h3>服务器</h3>
    <p>
      server_id: <code>{{ session.wellKnown.server_id }}</code>
      <span class="muted">（稳定身份，CLI 绑定主键）</span>
    </p>
    <p>canonical_url: {{ session.wellKnown.canonical_url }}</p>
    <p>protocol_version: {{ session.wellKnown.protocol_version }}</p>
  </div>

  <div v-if="session.isLoggedIn" class="card">
    <h3>我的 Workspace</h3>
    <p v-if="error" style="color: #b3261e">{{ error }}</p>
    <ul v-if="workspaces.length">
      <li v-for="ws in workspaces" :key="ws.id">
        <RouterLink :to="`/workspaces/${ws.id}`">{{ ws.name }}</RouterLink>
        <span class="muted">（{{ ws.slug }}）</span>
      </li>
    </ul>
    <p v-else class="muted">还没有可访问的 workspace；可先用 CLI：<code>astral init &lt;server&gt;</code></p>
  </div>
  <div v-else-if="session.booted" class="card">
    <p class="muted">
      未登录。<RouterLink to="/login">登录</RouterLink>
      后可管理 workspace；CLI 用户通过 <code>astral login</code> 设备授权。
    </p>
  </div>

  <div v-if="session.capabilities" class="card">
    <h3>能力</h3>
    <p>minimum_cli_version: {{ session.capabilities.minimum_cli_version }}</p>
    <pre>{{ JSON.stringify(session.capabilities.features, null, 2) }}</pre>
  </div>
</template>
