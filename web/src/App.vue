<script setup lang="ts">
// 顶层布局：侧边导航 + 路由出口。
// TODO(phase-1): 登录态驱动导航（未登录只显示 /device 审批入口）。
// TODO(phase-6): UI 框架选型（docs/看我看我.md「ui框架待定」）；当前用极简手写样式。
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <h1 class="brand">Astral</h1>
      <nav>
        <RouterLink to="/">总览</RouterLink>
        <!-- TODO(phase-3): 任务树 /workspaces/:id/tasks -->
        <!-- TODO(phase-5): 冲突处理 /workspaces/:id/conflicts -->
        <!-- TODO(phase-6): 审计 /workspaces/:id/audit -->
        <RouterLink to="/device">设备授权</RouterLink>
      </nav>
    </aside>
    <main class="content">
      <RouterView />
    </main>
  </div>
</template>
