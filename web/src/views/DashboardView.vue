<script setup lang="ts">
// 主页（原总览）：登录用户的个人工作台——「我的工作区」入口为主位，
// 「我的工作」汇总个人维度的全局页面；服务器元信息收敛为页脚一行小字
// （原 ServerCard / CapabilitiesCard 大卡片撤下）。匿名访问给登录/注册
// 引导，注册链接透传 ?code= 邀请码（RegisterView 从 query 读取预填）。
// 演示身份（isDemo）不消费真实 API：工作区段隐藏，其余照常。
import { computed, markRaw, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { FolderOpenIcon, KeyRoundIcon, PlusIcon, UserRoundIcon, UsersRoundIcon } from '@lucide/vue'
import CreateWorkspaceDialog from '@/components/layout/CreateWorkspaceDialog.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { fmtRelative, fmtTime } from '@/lib/format'
import { useSessionStore } from '@/stores/session'
import { useWorkspacesStore } from '@/stores/workspaces'

const session = useSessionStore()
const workspaces = useWorkspacesStore()
const route = useRoute()

const createOpen = ref(false)

// 路由守卫已保证 boot 完成后再挂载；mock 演示身份跳过真实 API
// （401 会触发全局登出弹走）。
onMounted(() => {
  if (session.isLoggedIn && !session.isDemo) void workspaces.load()
})

// 注册码直达：主页收到的 ?code= 原样带给 /register（它从自身 query 预填）。
const registerLocation = computed(() => {
  const code = route.query.code
  return typeof code === 'string' && code ? { path: '/register', query: { code } } : { path: '/register' }
})

// 「我的工作」：个人维度的全局页面入口。平台管理入口仅 admin 可见（与侧栏收敛一致）。
const personalLinks = computed(() => {
  const links = [
    {
      to: '/device',
      icon: markRaw(KeyRoundIcon),
      label: '设备管理',
      description: '登录设备与会话，批准新设备',
    },
    {
      to: '/profile',
      icon: markRaw(UserRoundIcon),
      label: '个人资料',
      description: '头像、显示名与个性签名',
    },
  ]
  if (session.isPlatformAdmin) {
    links.push({
      to: '/admin/users',
      icon: markRaw(UsersRoundIcon),
      label: '用户管理',
      description: '平台账号、停用恢复与角色',
    })
  }
  return links
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <PageHeader title="主页" :description="session.isLoggedIn ? '你的工作区与个人入口' : undefined" />

    <!-- 匿名：简明登录/注册引导（服务器信息只在页脚留一行小字）。 -->
    <Card v-if="!session.isLoggedIn" class="max-w-xl">
      <CardHeader>
        <CardTitle>人与 agent 协作的任务工作台</CardTitle>
        <CardDescription>
          登录后管理你的工作区与任务；收到注册邀请码的用户可注册账号，加入工作区另行兑换工作区邀请码。
        </CardDescription>
      </CardHeader>
      <CardContent class="flex flex-wrap items-center gap-3">
        <Button as-child>
          <RouterLink to="/login">登录</RouterLink>
        </Button>
        <Button variant="outline" as-child>
          <RouterLink :to="registerLocation">注册码注册</RouterLink>
        </Button>
        <p class="text-muted-foreground w-full text-xs">
          CLI 用户运行 <code>astral login</code> 登录，批准入口在登录后的「设备管理」页。
        </p>
      </CardContent>
    </Card>

    <!-- 登录：工作区主位 + 个人入口侧栏；演示身份隐藏工作区段。 -->
    <div
      v-else
      class="grid items-start gap-6"
      :class="session.isDemo ? 'max-w-md' : 'lg:grid-cols-[minmax(0,1fr)_280px]'"
    >
      <section v-if="!session.isDemo" class="flex min-w-0 flex-col gap-3">
        <div class="flex items-center justify-between gap-3">
          <h3 class="text-sm font-medium">我的工作区</h3>
          <Button variant="outline" size="sm" @click="createOpen = true">
            <PlusIcon />
            新建工作区
          </Button>
        </div>

        <div v-if="!workspaces.loaded" class="grid gap-3 sm:grid-cols-2">
          <Skeleton v-for="i in 4" :key="i" class="h-24 rounded-xl" />
        </div>

        <ul v-else-if="workspaces.items.length" class="grid gap-3 sm:grid-cols-2">
          <li v-for="ws in workspaces.items" :key="ws.id">
            <RouterLink
              :to="`/workspaces/${ws.id}`"
              class="flex h-24 flex-col justify-between gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10 transition-colors outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span class="truncate text-sm font-medium">{{ ws.name }}</span>
              <span class="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                <span class="truncate font-mono">{{ ws.slug }}</span>
                <span class="shrink-0" :title="`创建于 ${fmtTime(ws.created_at)}`">
                  创建于 {{ fmtRelative(ws.created_at) }}
                </span>
              </span>
            </RouterLink>
          </li>
        </ul>

        <Empty v-else>
          <EmptyMedia variant="icon">
            <FolderOpenIcon />
          </EmptyMedia>
          <EmptyHeader>
            <EmptyTitle>还没有可访问的工作区</EmptyTitle>
            <EmptyDescription>创建第一个工作区，或让已有成员把你加入。</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button @click="createOpen = true">
              <PlusIcon />
              新建工作区
            </Button>
          </EmptyContent>
        </Empty>
      </section>

      <aside class="flex min-w-0 flex-col gap-3">
        <h3 class="text-sm font-medium">我的工作</h3>
        <Card class="gap-0 py-0">
          <ul class="divide-y">
            <li v-for="link in personalLinks" :key="link.to">
              <RouterLink
                :to="link.to"
                class="flex items-start gap-3 p-4 transition-colors outline-none hover:bg-muted/50 focus-visible:bg-muted/50"
              >
                <component :is="link.icon" class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <span class="flex min-w-0 flex-col gap-0.5">
                  <span class="text-sm font-medium">{{ link.label }}</span>
                  <span class="text-muted-foreground text-xs">{{ link.description }}</span>
                </span>
              </RouterLink>
            </li>
          </ul>
        </Card>
      </aside>
    </div>

    <CreateWorkspaceDialog v-if="!session.isDemo" v-model:open="createOpen" />

    <!-- 服务器元信息：仅保留辨识所需的版本与身份，一行小字。 -->
    <footer
      v-if="session.wellKnown"
      class="flex flex-wrap gap-x-4 gap-y-1 border-t pt-4 text-xs text-muted-foreground"
    >
      <span>Astral</span>
      <span>协议 v{{ session.wellKnown.protocol_version }}</span>
      <span v-if="session.capabilities">CLI ≥ {{ session.capabilities.minimum_cli_version }}</span>
      <span class="font-mono">{{ session.wellKnown.server_id }}</span>
    </footer>
  </div>
</template>
