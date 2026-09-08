<script setup lang="ts">
// 顶层布局：会话引导（boot）+ 导航 + 登录态展示。
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useSessionStore } from './stores/session'

const session = useSessionStore()
const router = useRouter()

onMounted(() => {
  void session.boot()
})

async function logout(): Promise<void> {
  await session.logout()
  void router.push('/')
}
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <h1 class="brand">Astral</h1>
      <nav>
        <RouterLink to="/">总览</RouterLink>
        <RouterLink to="/device">设备授权</RouterLink>
      </nav>
      <div class="session-box">
        <template v-if="session.isLoggedIn">
          <p class="muted">{{ session.actor?.display_name }}</p>
          <button @click="logout">登出</button>
        </template>
        <template v-else-if="session.booted">
          <RouterLink to="/login">登录</RouterLink>
        </template>
      </div>
    </aside>
    <main class="content">
      <p v-if="session.bootError" class="card">
        无法连接 astral-server：{{ session.bootError }}（请确认 server 已在 :8080 监听）
      </p>
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
.session-box {
  margin-top: 24px;
  font-size: 13px;
}
.session-box button {
  background: #2a2d33;
  color: #fff;
  border: 0;
  padding: 4px 10px;
  border-radius: 6px;
  cursor: pointer;
}
</style>
