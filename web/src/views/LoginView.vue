<script setup lang="ts">
// Human 本地登录页（TODO.md D6）。登录成功后跳回来源页或总览。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useApiAction } from '@/composables/useApiAction'
import { useLoginRedirect } from '@/composables/useLoginRedirect'
import { useSessionStore } from '@/stores/session'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

const session = useSessionStore()
const router = useRouter()
const { consumeRedirect } = useLoginRedirect()
const { busy, error, run } = useApiAction()

const email = ref('')
const password = ref('')

async function submit(): Promise<void> {
  const ok = await run(() => session.login(email.value, password.value))
  if (ok) void router.push(consumeRedirect() ?? '/')
}
</script>

<template>
  <div class="mx-auto flex w-full max-w-md flex-col gap-4">
    <PageHeader title="登录" />
    <Card>
      <CardContent>
        <form class="flex flex-col gap-4" @submit.prevent="submit">
          <ErrorAlert v-if="error" :message="error" />
          <FieldGroup>
            <Field>
              <FieldLabel for="email">邮箱</FieldLabel>
              <Input
                id="email"
                v-model="email"
                type="email"
                required
                autocomplete="username"
              />
            </Field>
            <Field>
              <FieldLabel for="password">密码</FieldLabel>
              <Input
                id="password"
                v-model="password"
                type="password"
                required
                autocomplete="current-password"
              />
            </Field>
          </FieldGroup>
          <Button type="submit" :disabled="busy">
            <Spinner v-if="busy" data-icon="inline-start" />
            {{ busy ? '登录中…' : '登录' }}
          </Button>
        </form>
      </CardContent>
      <CardFooter>
        <p class="text-muted-foreground text-sm">
          首个账号通过服务器初始化时的 bootstrap 注册创建；CLI 登录请使用
          <code>astral login</code>（设备授权流程），审批入口在「设备授权」页。
        </p>
      </CardFooter>
    </Card>
  </div>
</template>
