import { createRouter, createWebHistory } from 'vue-router'
import { useSessionStore } from '../stores/session'
import { sanitizeRedirect } from '../lib/redirect'
import MainLayout from '../components/layout/MainLayout.vue'

// 路由规划对齐 roadmap Phase 5（GUI 监控/干预面）。
// /device 是 Device Flow 的人类审批页 —— verification_uri 指向这里（architecture §8.2），
// 属 CLI 登录闭环的必要组成，Phase 1 就要实装。
//
// 结构：/login 独立布局（无侧边栏），其余页面统一挂 '/' 下由 MainLayout 承载。
// 鉴权策略用 meta.auth 声明，「未登录去哪」只由下方全局守卫决策，视图层不再各自跳转。
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
    {
      path: '/',
      component: MainLayout,
      children: [
        {
          path: '',
          name: 'dashboard',
          component: () => import('../views/DashboardView.vue'),
          meta: { auth: 'optional' },
        },
        {
          path: 'device',
          name: 'device-approve',
          component: () => import('../views/DeviceApproveView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId',
          name: 'workspace-overview',
          component: () => import('../views/WorkspaceOverviewView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId/approvals',
          name: 'workspace-approvals',
          component: () => import('../views/ApprovalsView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: ':pathMatch(.*)*',
          name: 'not-found',
          component: () => import('../views/NotFoundView.vue'),
        },
      ],
    },
  ],
})

declare module 'vue-router' {
  interface RouteMeta {
    /** required=未登录跳 /login 并带 from；optional/缺省=匿名可看，视图自行降级 */
    auth?: 'required' | 'optional'
  }
}

// 全局守卫：先等会话 boot 完成（幂等且并发安全，见 stores/session），
// 再按 meta.auth 决定放行。守卫是登录态判断的唯一入口，解决两个问题：
// 1) 视图挂载时登录态已就绪（不会拿 boot 中间态做跳转决策）；
// 2) 未登录访问受保护页统一跳 /login?from=<fullPath>，登录后原路返回。
router.beforeEach(async (to) => {
  const session = useSessionStore()
  await session.boot()

  if (to.meta.auth === 'required' && !session.isLoggedIn) {
    return { path: '/login', query: { from: to.fullPath } }
  }
  // 已登录访问 /login：直接送去 from 目标或总览（sanitize 已排除 /login 自身，无循环）。
  if (to.name === 'login' && session.isLoggedIn) {
    return sanitizeRedirect(to.query.from) ?? '/'
  }
})
