<script setup lang="ts">
// 注册码注册页（TODO.md A5 / docs/registration.md §5.1 / ADR-0009 双轨分离）。
// 独立布局（不套 MainLayout），与 /login、/device 同构，便于从邮件/聊天链接直达。
// 匿名专属：已登录由路由守卫弹走。?code= 预填注册邀请码（注册邀请链接直达场景），
// 也支持手动输入；注册只建号不入伙——加入工作区需另行兑换工作区邀请码
// （登录后侧栏「加入工作区」）。注册成功即登录（服务端已建会话），跳回 from 或总览。
//
// 二次密码确认（2026-10-01 生产 case：注册口令输错 → 无法登录且无自助恢复）：
// 两次不一致就地拦截，不发起请求；密码框带小眼睛可在提交前核对明文。
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useApiAction } from '@/composables/useApiAction'
import { useLoginRedirect } from '@/composables/useLoginRedirect'
import { usePasswordConfirm } from '@/composables/usePasswordConfirm'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/shared/password-input'
import { Spinner } from '@/components/ui/spinner'

const session = useSessionStore()
const route = useRoute()
const router = useRouter()
const { consumeRedirect } = useLoginRedirect()
const { busy, run } = useApiAction()

const registrationCode = ref(typeof route.query.code === 'string' ? route.query.code : '')
const email = ref('')
const displayName = ref('')
const { password, passwordConfirm, confirmMismatch, ensureMatch } = usePasswordConfirm()

async function submit(): Promise<void> {
  if (!ensureMatch()) return
  const ok = await run(() =>
    session.register({
      email: email.value,
      password: password.value,
      display_name: displayName.value || undefined,
      registration_code: registrationCode.value || undefined,
    }),
  )
  if (ok) void router.push(consumeRedirect() ?? '/')
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="flex w-full max-w-md flex-col gap-4">
      <PageHeader title="注册" description="使用注册邀请码创建账号并自动登录。" />
      <Card>
        <CardContent>
          <form class="flex flex-col gap-4" @submit.prevent="submit">
            <FieldGroup>
              <Field>
                <FieldLabel for="registration-code">注册邀请码</FieldLabel>
                <Input
                  id="registration-code"
                  v-model="registrationCode"
                  class="font-mono"
                  placeholder="XXXXX-XXXXX-XXXXX-XXXXX"
                  autocomplete="off"
                />
              </Field>
              <Field>
                <FieldLabel for="email">邮箱</FieldLabel>
                <Input id="email" v-model="email" type="email" required autocomplete="username" />
              </Field>
              <Field>
                <FieldLabel for="password">密码</FieldLabel>
                <PasswordInput
                  id="password"
                  v-model="password"
                  required
                  minlength="8"
                  autocomplete="new-password"
                />
              </Field>
              <Field>
                <FieldLabel for="password-confirm">确认密码</FieldLabel>
                <PasswordInput
                  id="password-confirm"
                  v-model="passwordConfirm"
                  required
                  minlength="8"
                  autocomplete="new-password"
                  :aria-invalid="confirmMismatch || undefined"
                />
                <p
                  v-if="confirmMismatch"
                  class="text-destructive text-sm"
                  role="alert"
                >
                  两次输入的密码不一致
                </p>
              </Field>
              <Field>
                <FieldLabel for="display-name">显示名（可选）</FieldLabel>
                <Input id="display-name" v-model="displayName" autocomplete="nickname" />
              </Field>
            </FieldGroup>
            <Button type="submit" :disabled="busy || confirmMismatch">
              <Spinner v-if="busy" data-icon="inline-start" />
              {{ busy ? '注册中…' : '注册' }}
            </Button>
          </form>
        </CardContent>
        <CardFooter>
          <p class="text-muted-foreground text-sm">
            注册邀请码由平台管理员签发，一次性有效（失效/已用/撤销均同提示）。注册只创建账号，
            不加入任何工作区——加入工作区请在登录后用侧栏「加入工作区」兑换工作区邀请码。
            无注册码时本页仅在服务器还没有任何账号时可用（bootstrap）；CLI 登录请用
            <code>astral login</code>（设备授权流程）。
          </p>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
