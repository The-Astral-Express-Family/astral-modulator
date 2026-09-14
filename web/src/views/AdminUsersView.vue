<script setup lang="ts">
// 平台用户管理页（round 34，/admin/users）：human 账号列表 + 停用/恢复 +
// 平台角色变更。数据加载本身就是能力证明：list 403/404 → 无权访问态
// （fail closed，对齐 InvitationsCard——前端不复制角色判断逻辑）。
// 自我保护由服务端强制（禁自停用/自改角色），这里对"自己"这行直接收起操作。
import { onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import {
  changeAdminUserRole,
  disableAdminUser,
  enableAdminUser,
  listAdminUsers,
} from '@/api/modules/admin'
import type { AdminUser, PlatformRole } from '@/api/types'
import { fmtTime } from '@/lib/format'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useApiAction } from '@/composables/useApiAction'
import { useSessionStore } from '@/stores/session'

type HumanRole = Exclude<PlatformRole, 'agent' | 'service'>

const session = useSessionStore()
// null = 加载中；false = 无权访问（403/404）；true = 正常渲染。
const canAccess = ref<boolean | null>(null)
const users = ref<AdminUser[]>([])
const busyId = ref<string | null>(null)
const { run } = useApiAction()

const ROLE_VARIANTS: Record<PlatformRole, BadgeVariants['variant']> = {
  admin: 'default',
  user: 'secondary',
  agent: 'outline',
  service: 'outline',
}
const ROLE_LABELS: Record<PlatformRole, string> = {
  admin: '管理员',
  user: '普通用户',
  agent: 'Agent',
  service: 'Service',
}

onMounted(async () => {
  try {
    users.value = (await listAdminUsers()).items
    canAccess.value = true
  } catch {
    canAccess.value = false
  }
})

function isSelf(u: AdminUser): boolean {
  return u.id === session.actor?.id
}

async function toggleDisabled(u: AdminUser): Promise<void> {
  busyId.value = u.id
  const disabling = u.disabled_at == null
  await run(async () => {
    if (disabling) {
      await disableAdminUser(u.id)
      u.disabled_at = new Date().toISOString()
      toast.success(`已停用 ${u.display_name}`)
    } else {
      await enableAdminUser(u.id)
      u.disabled_at = undefined
      toast.success(`已恢复 ${u.display_name}`)
    }
  })
  busyId.value = null
}

async function changeRole(u: AdminUser, role: HumanRole): Promise<void> {
  if (role === u.platform_role) return
  busyId.value = u.id
  await run(async () => {
    const updated = await changeAdminUserRole(u.id, role)
    u.platform_role = updated.platform_role
    toast.success(`${u.display_name} 已设为 ${ROLE_LABELS[updated.platform_role]}`)
  })
  busyId.value = null
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader
      title="用户管理"
      description="平台级账号列表、停用/恢复与角色变更（仅管理员可见）"
    />

    <div v-if="canAccess === null" class="flex justify-center py-16">
      <Spinner class="size-6" />
    </div>

    <Card v-else-if="canAccess === false">
      <CardContent class="py-10 text-center text-sm text-muted-foreground">
        无权访问平台管理功能。此页面仅对平台管理员开放。
      </CardContent>
    </Card>

    <template v-else>
      <Card>
        <CardHeader>
          <CardTitle>平台用户（{{ users.length }}）</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户</TableHead>
                <TableHead>邮箱</TableHead>
                <TableHead>平台角色</TableHead>
                <TableHead>注册时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead class="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="u in users"
                :key="u.id"
                :class="{ 'opacity-60': u.disabled_at != null }"
              >
                <TableCell class="font-medium">{{ u.display_name }}</TableCell>
                <TableCell class="text-muted-foreground">{{ u.email ?? '—' }}</TableCell>
                <TableCell>
                  <Badge :variant="ROLE_VARIANTS[u.platform_role]">
                    {{ ROLE_LABELS[u.platform_role] }}
                  </Badge>
                </TableCell>
                <TableCell class="text-muted-foreground">{{ fmtTime(u.created_at) }}</TableCell>
                <TableCell>
                  <Badge v-if="u.disabled_at != null" variant="destructive">已停用</Badge>
                  <span v-else class="text-sm text-muted-foreground">正常</span>
                </TableCell>
                <TableCell class="text-right">
                  <template v-if="isSelf(u)">
                    <span class="text-sm text-muted-foreground">（当前账号）</span>
                  </template>
                  <div v-else class="flex items-center justify-end gap-2">
                    <Select
                      :model-value="u.platform_role"
                      :disabled="busyId === u.id"
                      @update:model-value="(v) => changeRole(u, v as HumanRole)"
                    >
                      <SelectTrigger class="h-8 w-28">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="admin">管理员</SelectItem>
                        <SelectItem value="user">普通用户</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button
                      variant="outline"
                      size="sm"
                      :disabled="busyId === u.id"
                      @click="toggleDisabled(u)"
                    >
                      <Spinner v-if="busyId === u.id" data-icon="inline-start" />
                      {{ u.disabled_at != null ? '恢复' : '停用' }}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
