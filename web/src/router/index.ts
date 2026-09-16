import { createRouter, createWebHistory } from 'vue-router'
import { useSessionStore } from '../stores/session'
import { loginLocation, sanitizeRedirect } from '../lib/redirect'
import MainLayout from '../components/layout/MainLayout.vue'

// 路由规划对齐 roadmap Phase 5（GUI 监控/干预面）。
// /device 是 Device Flow 的人类审批页 —— verification_uri 指向这里（architecture §8.2），
// 属 CLI 登录闭环的必要组成，Phase 1 就要实装。
//
// 结构：/login 与 /device 独立布局（无侧边栏）——/device 是 CLI 拉起浏览器的
// 审批页，不应携带应用外壳；其余页面统一挂 '/' 下由 MainLayout 承载。
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
      path: '/device',
      name: 'device-approve',
      component: () => import('../views/DeviceApproveView.vue'),
      meta: { auth: 'required' },
    },
    {
      path: '/register',
      name: 'register',
      component: () => import('../views/RegisterView.vue'),
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
          path: 'profile',
          name: 'profile',
          component: () => import('../views/ProfileView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'admin/users',
          name: 'admin-users',
          component: () => import('../views/AdminUsersView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId',
          name: 'workspace-overview',
          component: () => import('../views/WorkspaceOverviewView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId/tasks',
          name: 'workspace-tasks',
          component: () => import('../views/TaskTreeView.vue'),
          meta: { auth: 'required', wide: true },
        },
        {
          path: 'workspaces/:workspaceId/approvals',
          name: 'workspace-approvals',
          component: () => import('../views/ApprovalsView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId/tags',
          name: 'workspace-tags',
          component: () => import('../views/TagManagerView.vue'),
          meta: { auth: 'required' },
        },
        {
          path: 'workspaces/:workspaceId/messages',
          name: 'workspace-messages',
          component: () => import('../views/MessagesView.vue'),
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
    /** wide=内容列放宽（任务树等双栏视图需要横向空间） */
    wide?: boolean
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
    return loginLocation(to.fullPath)
  }
  // 已登录访问 /login、/register：直接送去 from 目标或总览
  // （两页都是匿名专属动作；sanitize 已排除 /login 自身，无循环）。
  // mock 演示身份不算真实登录态，否则 VITE_TASKS_MOCK=1 的开发模式下
  // 匿名注册/登录入口会被误弹走。
  if ((to.name === 'login' || to.name === 'register') && session.isLoggedIn && !session.isDemo) {
    return sanitizeRedirect(to.query.from) ?? '/'
  }
})
