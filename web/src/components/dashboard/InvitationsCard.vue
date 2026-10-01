<script setup lang="ts">
// 工作区邀请管理卡（A5 P2 / ADR-0009）：签发 / 列表 / 复制链接 / 撤销。
// 码是入伙资格：发给已注册用户，经 /join 或侧栏「加入工作区」兑换。
// 可见性按能力收敛：listInvitations 403/404 即视为无 manage_members（或非成员），
// 整卡隐藏（fail closed，不在前端另复制一份角色判断逻辑）。
// 邮件投递（00024）：签发时可选填邮箱——链接与明文码同体同生命周期，
// 撤销/过期/兑换两渠道同时失效；同邮箱重签自动作废旧链接。
import { onMounted, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import {
  createInvitation,
  listInvitations,
  revokeInvitation,
} from '@/api/modules/workspace'
import type { Invitation, InvitationCreated } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useApiAction } from '@/composables/useApiAction'

const props = defineProps<{ workspaceId: string }>()

// 首次加载结果即能力判断：true 才渲染整卡（非管理者全程不可见）。
const canManage = ref(false)
const items = ref<Invitation[]>([])
const issued = ref<InvitationCreated | null>(null)
const role = ref<string>('contributor')
const ttl = ref<string>('604800')
const email = ref('')
const busyId = ref<string | null>(null)
const { busy: issuing, run } = useApiAction()

const ROLE_OPTIONS = [
  { value: 'viewer', label: 'viewer（只读）' },
  { value: 'contributor', label: 'contributor（读写）' },
  { value: 'maintainer', label: 'maintainer（管理）' },
]
const TTL_OPTIONS = [
  { value: '86400', label: '1 天' },
  { value: '604800', label: '7 天' },
  { value: '2592000', label: '30 天' },
]
const STATUS_VARIANTS: Record<Invitation['status'], BadgeVariants['variant']> = {
  invited: 'default',
  redeemed: 'secondary',
  revoked: 'outline',
}

async function load(): Promise<void> {
  try {
    items.value = (await listInvitations(props.workspaceId)).items
    canManage.value = true
  } catch {
    canManage.value = false // 403 无 manage_members / 404 非成员：整卡隐藏
  }
}

async function issue(): Promise<void> {
  const trimmed = email.value.trim()
  const ok = await run(async () => {
    issued.value = await createInvitation(props.workspaceId, {
      role: role.value as Invitation['role'],
      expires_in: Number(ttl.value),
      email: trimmed || undefined,
    })
    toast.success('邀请已签发')
  })
  if (ok) {
    email.value = ''
    await load()
  }
}

async function revoke(inv: Invitation): Promise<void> {
  busyId.value = inv.id
  const ok = await run(async () => {
    await revokeInvitation(inv.id)
    toast.success('邀请已撤销')
  })
  busyId.value = null
  if (ok) await load()
}

async function copy(text: string, what: string): Promise<void> {
  await navigator.clipboard.writeText(text)
  toast.success(`${what}已复制`)
}

onMounted(() => {
  void load()
})

watch(
  () => props.workspaceId,
  () => {
    issued.value = null
    void load()
  },
)
</script>

<template>
  <Card v-if="canManage">
    <CardHeader>
      <CardTitle>工作区邀请</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <form class="flex flex-col gap-2" @submit.prevent="issue">
        <div class="flex items-end gap-2">
          <Field>
            <FieldLabel for="invite-role">角色</FieldLabel>
            <Select v-model="role">
              <SelectTrigger id="invite-role" class="w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in ROLE_OPTIONS" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel for="invite-ttl">有效期</FieldLabel>
            <Select v-model="ttl">
              <SelectTrigger id="invite-ttl" class="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in TTL_OPTIONS" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Button type="submit" :disabled="issuing">
            {{ issuing ? '签发中…' : '签发邀请' }}
          </Button>
        </div>
        <Field>
          <FieldLabel for="invite-email">邮件送达（可选）</FieldLabel>
          <Input
            id="invite-email"
            v-model="email"
            type="email"
            placeholder="friend@example.com — 填写即同时把入伙链接与邀请码发到该邮箱"
          />
        </Field>
      </form>

      <!-- 签发结果：明文码与链接仅本次可见，重点呈现并给一键复制。 -->
      <div v-if="issued" class="flex flex-col gap-2 rounded-md border p-3">
        <div class="flex flex-wrap items-center gap-2">
          <code class="font-mono text-sm font-semibold">{{ issued.code }}</code>
          <Button size="sm" variant="outline" @click="copy(issued.code, '邀请码')">
            复制码
          </Button>
          <Button size="sm" variant="outline" @click="copy(issued.invite_url, '入伙链接')">
            复制链接
          </Button>
          <Badge v-if="issued.email" :variant="issued.email_sent_at ? 'default' : 'destructive'">
            {{ issued.email_sent_at ? `已邮件送达 ${issued.email}` : `邮件未送达 ${issued.email}` }}
          </Badge>
        </div>
        <p class="text-muted-foreground text-xs">
          {{ issued.invite_url }}
          明文码仅此一次展示，请立即发给已注册成员（码只入伙、不能注册）；泄露的处置是撤销。
          <template v-if="issued.email && !issued.email_sent_at">
            邮件投递失败（服务端未配置 SMTP 或投递出错）——请手动转发上面的链接/码，或重签重投。
          </template>
        </p>
      </div>

      <Table v-if="items.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>角色</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>邮箱</TableHead>
            <TableHead>签发时间</TableHead>
            <TableHead>过期时间</TableHead>
            <TableHead>兑换者</TableHead>
            <TableHead>操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="item in items" :key="item.id">
            <TableCell>
              <code class="font-mono text-xs">{{ item.role }}</code>
            </TableCell>
            <TableCell>
              <Badge :variant="STATUS_VARIANTS[item.status]">{{ item.status }}</Badge>
            </TableCell>
            <TableCell>
              <span v-if="item.email" class="text-sm">{{ item.email }}</span>
              <span v-else class="text-muted-foreground">站内分发</span>
            </TableCell>
            <TableCell>{{ fmtTime(item.created_at) }}</TableCell>
            <TableCell>{{ fmtTime(item.expires_at) }}</TableCell>
            <TableCell>
              <code v-if="item.redeemed_by" class="font-mono text-xs">{{ item.redeemed_by }}</code>
              <span v-else class="text-muted-foreground">—</span>
            </TableCell>
            <TableCell>
              <Button
                v-if="item.status === 'invited'"
                size="sm"
                variant="destructive"
                :disabled="busyId === item.id"
                @click="revoke(item)"
              >
                撤销
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      <p v-else class="text-muted-foreground text-sm">暂无邀请记录。</p>
    </CardContent>
  </Card>
</template>
