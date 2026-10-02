<!-- 重置用户密码（协议 2.6）：用户管理页行内入口。管理员直接设置新口令，
     目标全部会话（浏览器与 CLI）即时注销；新口令需经其他渠道告知。
     自我保护由服务端强制（禁自重置），入口层 self 行本就收起操作。 -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import { resetAdminUserPassword } from '@/api/modules/admin'
import type { AdminUser } from '@/api/types'
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
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { useApiAction } from '@/composables/useApiAction'

const props = defineProps<{ open: boolean; user: AdminUser | null }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const newPassword = ref('')
const { busy, run } = useApiAction()

watch(
  () => props.open,
  (open) => {
    if (open) newPassword.value = ''
  },
)

// 与登录/自助改密同一口径的前置校验（服务端策略：≥8 位、含字母与数字）；
// 失败提示走本地 toast（不经过 API 拦截器）。
async function submit(): Promise<void> {
  if (!props.user) return
  const pwd = newPassword.value
  if (pwd.length < 8 || !/[a-zA-Z]/.test(pwd) || !/\d/.test(pwd)) {
    toast.error('新密码至少 8 位，需包含字母与数字')
    return
  }
  const ok = await run(async () => {
    await resetAdminUserPassword(props.user!.id, pwd)
  })
  if (ok) {
    toast.success(`已重置 ${props.user.display_name} 的密码，请通过其他渠道告知对方`)
    emit('update:open', false)
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>重置密码</DialogTitle>
        <DialogDescription>
          为 {{ user?.display_name ?? '该用户' }} 设置新密码。提交后其全部已登录会话（浏览器与
          CLI）立即注销，请务必通过其他渠道把新密码告知对方。
        </DialogDescription>
      </DialogHeader>
      <form class="flex flex-col gap-4" @submit.prevent="submit">
        <Field>
          <FieldLabel for="reset-password-input">新密码</FieldLabel>
          <Input
            id="reset-password-input"
            v-model="newPassword"
            type="password"
            required
            autocomplete="new-password"
            placeholder="至少 8 位，含字母与数字"
          />
        </Field>
        <DialogFooter>
          <Button type="button" variant="outline" @click="emit('update:open', false)">
            取消
          </Button>
          <Button type="submit" :disabled="busy">
            <Spinner v-if="busy" data-icon="inline-start" />
            {{ busy ? '重置中…' : '重置密码' }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
