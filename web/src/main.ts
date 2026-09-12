import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { setUnauthorizedHandler } from './api/client'
import { loginLocation } from './lib/redirect'
import { useSessionStore } from './stores/session'
import './assets/index.css'

// pinia 先于 router 安装：路由守卫在首次导航时要能取到 session store。
const pinia = createPinia()
const app = createApp(App)
app.use(pinia).use(router)

// 会话过期统一出口：登录态下任何 API 401 → 清本地会话并带 from 跳登录。
// 匿名期（boot 探测 / 登录失败）的 401 不处理，避免冷启动被误弹到登录页。
const session = useSessionStore(pinia)
setUnauthorizedHandler(() => {
  if (!session.isLoggedIn) return
  session.expireSession()
  const current = router.currentRoute.value
  if (current.path === '/login') return
  void router.push(loginLocation(current.fullPath))
})

app.mount('#app')
