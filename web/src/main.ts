import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { toast } from 'vue-sonner'
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

// 会话过期统一出口：登录态下任何 API 401 → 清本地会话 + 提示 + 带 from 跳登录，
// 返回 true 告知拦截器已消费（不再重复 toast）。匿名期（boot 探测 / 登录失败）
// 的 401 返回 false，由拦截器按普通错误提示。
const session = useSessionStore(pinia)
setUnauthorizedHandler(() => {
  if (!session.isLoggedIn) return false
  session.expireSession()
  toast.error('登录已过期，请重新登录。')
  const current = router.currentRoute.value
  if (current.path !== '/login') void router.push(loginLocation(current.fullPath))
  return true
})

app.mount('#app')
