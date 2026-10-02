<script setup lang="ts">
// 重置密码（00023）：从邮件链接 ?token=prt_… 落地。新密码 + 二次确认
// （PasswordInput 带小眼睛，与注册页同构——正是「注册口令输错」的补救面）。
// 成功后服务端已吊销全部会话：清本地会话态再跳 /login 重新登录。
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { useApiAction } from '@/composables/useApiAction'
import { usePasswordConfirm } from '@/composables/usePasswordConfirm'
import { confirmPasswordReset } from '@/api/modules/auth'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { PasswordInput } from '@/components/shared/password-input'
import { Spinner } from '@/components/ui/spinner'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const { busy, run } = useApiAction()

const token = typeof route.query.token === 'string' ? route.query.token : ''
const { password, passwordConfirm, confirmMismatch, ensureMatch } = usePasswordConfirm()

async function submit(): Promise<void> {
  if (!ensureMatch()) return
  const ok = await run(() => confirmPasswordReset(token, password.value))
  if (!ok) return
  toast.success('密码已重置，请重新登录')
  // 全部会话已被服务端吊销（含本浏览器）：清本地态，回到登录页。
  session.expireSession()
  void router.push('/login')
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="flex w-full max-w-md flex-col gap-4">
      <PageHeader title="重置密码" description="设置新密码（至少 8 位，含字母与数字）。" />
      <Card>
        <CardContent>
          <form v-if="token" class="flex flex-col gap-4" @submit.prevent="submit">
            <FieldGroup>
              <Field>
                <FieldLabel for="password">新密码</FieldLabel>
                <PasswordInput id="password" v-model="password" required minlength="8" autocomplete="new-password" />
              </Field>
              <Field>
                <FieldLabel for="password-confirm">确认新密码</FieldLabel>
                <PasswordInput
                  id="password-confirm"
                  v-model="passwordConfirm"
                  required
                  minlength="8"
                  autocomplete="new-password"
                  :aria-invalid="confirmMismatch || undefined"
                />
                <p v-if="confirmMismatch" class="text-destructive text-sm" role="alert">
                  两次输入的密码不一致
                </p>
              </Field>
            </FieldGroup>
            <Button type="submit" :disabled="busy || confirmMismatch">
              <Spinner v-if="busy" data-icon="inline-start" />
              {{ busy ? '重置中…' : '重置密码' }}
            </Button>
          </form>
          <p v-else class="text-destructive text-sm">
            链接缺少重置凭据——请从邮件中的完整链接进入，或
            <RouterLink to="/forgot-password" class="underline">重新申请</RouterLink>。
          </p>
        </CardContent>
        <CardFooter>
          <p class="text-muted-foreground text-sm">
            重置链接 30 分钟内有效且仅可使用一次；成功后该账号所有已登录会话
            （含 CLI 设备）将被注销。
          </p>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
