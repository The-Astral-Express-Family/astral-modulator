<!-- 生成注册邀请（平台级，00018）：用户管理页入口。只选有效期 → 签发 →
     同框切结果态（code/invite_url 明文仅此一次）。区别于 workspace 邀请：
     平台邀请只管「允许注册」，兑换后是普通 user，不入任何 workspace。
     最近签发列表：status badge + 撤销（服务端幂等）。 -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import {
  createRegistrationInvitation,
  listRegistrationInvitations,
  revokeRegistrationInvitation,
  type RegistrationInvitation,
  type RegistrationInvitationCreated,
} from '@/api/modules/admin'
import { fmtTime } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { useApiAction } from '@/composables/useApiAction'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const TTL_OPTIONS = [
  { value: '86400', label: '1 天' },
  { value: '604800', label: '7 天' },
  { value: '2592000', label: '30 天' },
]
const STATUS_VARIANTS: Record<RegistrationInvitation['status'], BadgeVariants['variant']> = {
  invited: 'secondary',
  redeemed: 'default',
  revoked: 'destructive',
}
const STATUS_LABELS: Record<RegistrationInvitation['status'], string> = {
  invited: '待使用',
  redeemed: '已兑换',
  revoked: '已撤销',
}

const ttl = ref('604800')
const issued = ref<RegistrationInvitationCreated | null>(null)
const recent = ref<RegistrationInvitation[]>([])
const listLoading = ref(false)
const revokingId = ref<string | null>(null)
const { busy: issuing, run } = useApiAction()

watch(
  () => props.open,
  (open) => {
    if (!open) return
    issued.value = null
    void loadRecent()
  },
)

async function loadRecent(): Promise<void> {
  listLoading.value = true
  try {
    recent.value = (await listRegistrationInvitations({ limit: 5 })).items
  } catch {
    recent.value = [] // silent：空态自行引导
  } finally {
    listLoading.value = false
  }
}

async function issue(): Promise<void> {
  const ok = await run(async () => {
    issued.value = await createRegistrationInvitation(Number(ttl.value))
  })
  if (ok) {
    toast.success('邀请已生成，请立即复制保存')
    void loadRecent()
  }
}

async function revoke(inv: RegistrationInvitation): Promise<void> {
  revokingId.value = inv.id
  const ok = await run(async () => {
    await revokeRegistrationInvitation(inv.id)
  })
  revokingId.value = null
  if (ok) {
    toast.success('已撤销邀请')
    void loadRecent()
  }
}

async function copy(text: string, what: string): Promise<void> {
  await navigator.clipboard.writeText(text)
  toast.success(`${what}已复制`)
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>生成注册邀请</DialogTitle>
        <DialogDescription>
          平台级邀请：兑换者注册为普通用户，不自动加入任何工作区。邀请码明文仅生成时显示一次。
        </DialogDescription>
      </DialogHeader>

      <!-- 表单态 -->
      <form v-if="!issued" class="flex flex-col gap-4" @submit.prevent="issue">
        <Field>
          <FieldLabel for="inv-ttl">有效期</FieldLabel>
          <Select v-model="ttl">
            <SelectTrigger id="inv-ttl">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="opt in TTL_OPTIONS" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </Field>

        <DialogFooter>
          <Button type="button" variant="outline" @click="emit('update:open', false)">
            取消
          </Button>
          <Button type="submit" :disabled="issuing">
            <Spinner v-if="issuing" data-icon="inline-start" />
            {{ issuing ? '生成中…' : '生成邀请' }}
          </Button>
        </DialogFooter>

        <!-- 最近签发：状态徽标 + 撤销 -->
        <div class="border-t pt-3">
          <div class="mb-2 flex items-center gap-2 text-sm font-medium">
            最近签发
            <Spinner v-if="listLoading" class="size-3.5" />
          </div>
          <p v-if="recent.length === 0 && !listLoading" class="text-xs text-muted-foreground">
            还没有平台邀请。生成第一个邀请码后，被邀请人即可通过链接注册。
          </p>
          <ul v-else class="flex flex-col gap-1.5">
            <li
              v-for="inv in recent"
              :key="inv.id"
              class="flex items-center justify-between gap-2 text-sm"
            >
              <span class="flex items-center gap-2">
                <Badge :variant="STATUS_VARIANTS[inv.status]">
                  {{ STATUS_LABELS[inv.status] }}
                </Badge>
                <span class="text-xs text-muted-foreground">
                  签发 {{ fmtTime(inv.created_at) }} · 过期 {{ fmtTime(inv.expires_at) }}
                </span>
              </span>
              <Button
                v-if="inv.status === 'invited'"
                size="sm"
                variant="ghost"
                :disabled="revokingId === inv.id"
                @click="revoke(inv)"
              >
                {{ revokingId === inv.id ? '撤销中…' : '撤销' }}
              </Button>
            </li>
          </ul>
        </div>
      </form>

      <!-- 结果态：code / invite_url 仅本次可见 -->
      <div v-else class="flex flex-col gap-4">
        <div class="flex flex-col gap-3 rounded-md border p-3">
          <div class="flex flex-col gap-1">
            <span class="text-xs font-medium text-muted-foreground">注册链接</span>
            <div class="flex items-start gap-2">
              <code class="flex-1 rounded bg-muted px-2 py-1.5 font-mono text-xs break-all">
                {{ issued.invite_url }}
              </code>
              <Button
                size="sm"
                variant="outline"
                class="shrink-0"
                @click="copy(issued.invite_url, '注册链接')"
              >
                复制链接
              </Button>
            </div>
          </div>

          <div class="flex flex-col gap-1">
            <span class="text-xs font-medium text-muted-foreground">邀请码</span>
            <div class="flex items-center gap-2">
              <code class="flex-1 rounded bg-muted px-2 py-1.5 font-mono text-sm font-semibold break-all">
                {{ issued.code }}
              </code>
              <Button
                size="sm"
                variant="outline"
                class="shrink-0"
                @click="copy(issued.code, '邀请码')"
              >
                复制码
              </Button>
            </div>
          </div>

          <dl class="grid grid-cols-[4.5rem_1fr] gap-x-3 gap-y-1 text-sm">
            <dt class="text-muted-foreground">有效期</dt>
            <dd>{{ TTL_OPTIONS.find((o) => o.value === ttl)?.label }}</dd>
            <dt class="text-muted-foreground">过期时间</dt>
            <dd>{{ fmtTime(issued.expires_at) }}</dd>
          </dl>
        </div>

        <p class="text-sm font-medium text-destructive">
          邀请码仅此一次显示，关闭对话框后无法再次查看，请立即复制分发。
        </p>

        <DialogFooter>
          <Button variant="outline" @click="emit('update:open', false)">关闭</Button>
          <Button @click="issued = null">再生成一个</Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>
