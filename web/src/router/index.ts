import { createRouter, createWebHistory } from 'vue-router'

// 路由规划对齐 roadmap Phase 5（GUI 监控/干预面）。
// /device 是 Device Flow 的人类审批页 —— verification_uri 指向这里（architecture §8.2），
// 属 CLI 登录闭环的必要组成，Phase 1 就要实装。
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: () => import('../views/DashboardView.vue'),
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
    {
      path: '/device',
      name: 'device-approve',
      component: () => import('../views/DeviceApproveView.vue'),
    },
    {
      path: '/workspaces/:workspaceId',
      name: 'workspace-overview',
      component: () => import('../views/WorkspaceOverviewView.vue'),
    },
    {
      path: '/workspaces/:workspaceId/tasks',
      name: 'workspace-tasks',
      component: () => import('../views/TaskTreeView.vue'),
    },
    {
      path: '/workspaces/:workspaceId/approvals',
      name: 'workspace-approvals',
      component: () => import('../views/ApprovalsView.vue'),
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('../views/NotFoundView.vue'),
    },
  ],
})
