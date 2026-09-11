<script setup lang="ts">
// Human 本地登录页（TODO.md D6）。登录成功后跳回来源页或总览。
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { formatApiError } from '../api/client'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const route = useRoute()
const router = useRouter()

const email = ref('')
const password = ref('')
const error = ref<string | null>(null)
const busy = ref(false)

async function submit(): Promise<void> {
  error.value = null
  busy.value = true
  try {
    await session.login(email.value, password.value)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    void router.push(redirect)
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <h2>登录</h2>
  <div class="card" style="max-width: 420px">
    <form @submit.prevent="submit">
      <p>
        <label>邮箱<br /><input v-model="email" type="email" required autocomplete="username" style="width: 100%" /></label>
      </p>
      <p>
        <label>密码<br /><input v-model="password" type="password" required autocomplete="current-password" style="width: 100%" /></label>
      </p>
      <p v-if="error" class="error-text">{{ error }}</p>
      <button type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
    </form>
    <p class="muted">
      首个账号通过服务器初始化时的 bootstrap 注册创建；CLI 登录请使用
      <code>astral login</code>（设备授权流程），审批入口在「设备授权」页。
    </p>
  </div>
</template>
