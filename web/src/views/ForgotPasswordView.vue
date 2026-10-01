<script setup lang="ts">
// 忘记密码（00023）：输入邮箱请求重置邮件。服务端恒 204 防枚举——
// 无论邮箱是否存在，界面都显示同一份「已发送」文案，不泄露账号存在性。
// 独立布局，与 /login 同构；提交后停留本页（可重复提交，新请求会作废
// 旧邮件里的链接）。
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { useApiAction } from '@/composables/useApiAction'
import { requestPasswordReset } from '@/api/modules/auth'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

const { busy, run } = useApiAction()
const email = ref('')
const requested = ref(false)

async function submit(): Promise<void> {
  const ok = await run(async () => {
    await requestPasswordReset(email.value)
    requested.value = true
    toast.success('如果该邮箱存在账号，重置邮件已发送')
  })
  void ok
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="flex w-full max-w-md flex-col gap-4">
      <PageHeader title="忘记密码" description="输入注册邮箱，我们会发送一次性重置链接。" />
      <Card>
        <CardContent>
          <form class="flex flex-col gap-4" @submit.prevent="submit">
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
            </FieldGroup>
            <Button type="submit" :disabled="busy">
              <Spinner v-if="busy" data-icon="inline-start" />
              {{ busy ? '发送中…' : '发送重置邮件' }}
            </Button>
          </form>
          <p v-if="requested" class="text-muted-foreground mt-3 text-sm">
            请求已受理。如果该邮箱存在账号，将收到含重置链接的邮件（30 分钟内有效、
            仅可使用一次）。没有收到可以重新发送——新邮件会使旧链接立即失效。
          </p>
        </CardContent>
        <CardFooter>
          <p class="text-muted-foreground text-sm">
            想起密码了？
            <RouterLink to="/login" class="text-primary hover:underline">返回登录</RouterLink>
            。
          </p>
        </CardFooter>
      </Card>
    </div>
  </div>
</template>
