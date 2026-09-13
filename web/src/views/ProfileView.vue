<script setup lang="ts">
// 个人资料页：头像外链 URL / 用户名 / 个性签名（PATCH /auth/me，部分更新契约）。
// 校验规则与服务端 profile.go 对齐；邮箱是登录身份，只读展示。
import { computed, reactive } from 'vue'
import { toast } from 'vue-sonner'
import { updateMe } from '@/api/modules/auth'
import type { PlatformRole } from '@/api/types'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
import UserAvatar from '@/components/shared/UserAvatar.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { useApiAction } from '@/composables/useApiAction'
import { useSessionStore } from '@/stores/session'

const MAX_DISPLAY_NAME = 200
const MAX_BIO = 500
const MAX_AVATAR_URL = 500

// 平台角色只读展示（round 33）；human 只会出现 admin/user 两种。
const ROLE_LABELS: Record<PlatformRole, string> = {
  admin: '管理员',
  user: '普通用户',
  agent: 'Agent',
  service: 'Service',
}

const session = useSessionStore()
const { busy, error, run } = useApiAction()

const form = reactive({
  display_name: session.actor?.display_name ?? '',
  bio: session.actor?.bio ?? '',
  avatar_url: session.actor?.avatar_url ?? '',
})

// 头像即时预览：输入中的资料先反映到预览（未保存也能看到效果）。
const previewActor = computed(() => ({
  id: session.actor?.id ?? '',
  display_name: form.display_name,
  avatar_url: form.avatar_url.trim(),
}))

const bioLength = computed(() => Array.from(form.bio).length)

function validate(): string | null {
  if (!form.display_name.trim()) return '用户名不能为空'
  if (Array.from(form.display_name.trim()).length > MAX_DISPLAY_NAME)
    return `用户名不能超过 ${MAX_DISPLAY_NAME} 字`
  if (bioLength.value > MAX_BIO) return `个性签名不能超过 ${MAX_BIO} 字`
  const url = form.avatar_url.trim()
  if (url) {
    if (url.length > MAX_AVATAR_URL) return '头像 URL 过长'
    try {
      const parsed = new URL(url)
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:')
        return '头像 URL 仅支持 http(s) 链接'
    } catch {
      return '头像 URL 格式不正确'
    }
  }
  return null
}

async function save(): Promise<void> {
  const problem = validate()
  if (problem) {
    error.value = problem
    return
  }
  await run(async () => {
    const me = await updateMe({
      display_name: form.display_name.trim(),
      bio: form.bio,
      avatar_url: form.avatar_url.trim(),
    })
    session.actor = me.actor
    if (me.email !== undefined) session.email = me.email
    // 以服务端 trim 后的值回填，避免表单与服务端状态漂移。
    form.display_name = me.actor.display_name
    form.bio = me.actor.bio
    form.avatar_url = me.actor.avatar_url
    toast.success('资料已保存')
  })
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="个人资料" description="头像、用户名与个性签名" />
    <Card>
      <CardContent>
        <form class="flex flex-col gap-6" @submit.prevent="save">
          <ErrorAlert v-if="error" :message="error" />
          <div class="flex items-center gap-4">
            <UserAvatar :actor="previewActor" class="size-16" fallback-class="text-2xl" />
            <div class="min-w-0 flex-1">
              <Field>
                <FieldLabel for="avatar_url">头像 URL</FieldLabel>
                <Input
                  id="avatar_url"
                  v-model="form.avatar_url"
                  type="url"
                  placeholder="https://…（留空使用首字母头像）"
                />
              </Field>
            </div>
          </div>
          <FieldGroup>
            <Field>
              <FieldLabel for="display_name">用户名</FieldLabel>
              <Input id="display_name" v-model="form.display_name" required :maxlength="MAX_DISPLAY_NAME" />
            </Field>
            <Field>
              <FieldLabel for="bio">个性签名</FieldLabel>
              <Textarea
                id="bio"
                v-model="form.bio"
                :maxlength="MAX_BIO"
                rows="3"
                placeholder="一句话介绍自己"
              />
              <p class="text-right text-xs text-muted-foreground">{{ bioLength }}/{{ MAX_BIO }}</p>
            </Field>
            <Field>
              <FieldLabel>邮箱</FieldLabel>
              <p class="rounded-md border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
                {{ session.email ?? '—' }}
              </p>
            </Field>
            <Field>
              <FieldLabel>平台角色</FieldLabel>
              <p class="rounded-md border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
                {{ ROLE_LABELS[session.platformRole ?? 'user'] }}
              </p>
            </Field>
          </FieldGroup>
          <Button type="submit" :disabled="busy" class="self-start">
            <Spinner v-if="busy" data-icon="inline-start" />
            {{ busy ? '保存中…' : '保存' }}
          </Button>
        </form>
      </CardContent>
      <CardFooter>
        <p class="text-sm text-muted-foreground">
          邮箱是登录身份，暂不支持在此修改；头像仅支持 http(s) 外链，链接失效时会回退为首字母头像。
        </p>
      </CardFooter>
    </Card>
  </div>
</template>
