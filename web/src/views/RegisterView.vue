<script setup lang="ts">
// 邀请码注册页（TODO.md A5 / docs/registration.md §5.1）。独立布局（不套 MainLayout），
// 与 /login、/device 同构，便于从邮件/聊天链接直达。匿名专属：已登录由路由守卫弹走。
// ?code= 预填邀请码（invite_url 直达场景），也支持手动输入；注册成功即登录
// （服务端已建会话），跳回 from 或总览。CLI 用户注册后请用 astral login 走设备流。
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useApiAction } from '@/composables/useApiAction'
import { useLoginRedirect } from '@/composables/useLoginRedirect'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

const session = useSessionStore()
const route = useRoute()
const router = useRouter()
const { consumeRedirect } = useLoginRedirect()
const { busy, run } = useApiAction()

const inviteCode = ref(typeof route.query.code === 'string' ? route.query.code : '')
const email = ref('')
const password = ref('')
const displayName = ref('')

async function submit(): Promise<void> {
  const ok = await run(() =>
    session.register({
      email: email.value,
      password: password.value,
      display_name: displayName.value || undefined,
      invite_code: inviteCode.value || undefined,
    }),
  )
  if (ok) void router.push(consumeRedirect() ?? '/')
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="flex w-full max-w-md flex-col gap-4">
      <PageHeader title="注册" description="注册即加入邀请所属的工作区，并自动登录。" />
      <Card>
        <CardContent>
          <form class="flex flex-col gap-4" @submit.prevent="submit">
            <FieldGroup>
              <Field>
                <FieldLabel for="invite-code">邀请码</FieldLabel>
                <Input
                  id="invite-code"
                  v-model="inviteCode"
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
                <Input
                  id="password"
                  v-model="password"
                  type="password"
                  required
                  minlength="8"
                  autocomplete="new-password"
                />
              </Field>
              <Field>
                <FieldLabel for="display-name">显示名（可选）</FieldLabel>
                <Input id="display-name" v-model="displayName" autocomplete="nickname" />
              </Field>
            </FieldGroup>
            <Button type="submit" :disabled="busy">
              <Spinner v-if="busy" data-icon="inline-start" />
              {{ busy ? '注册中…' : '注册' }}
            </Button>
          </form>
        </CardContent>
        <CardFooter>
          <p class="text-muted-foreground text-sm">
            邀请码由工作区管理员签发，一次性有效（失效/已用/撤销均同提示）。无邀请码时本页仅在
            服务器还没有任何账号时可用（bootstrap）；CLI 登录请用
            <code>astral login</code>（设备授权流程）。
          </p>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
