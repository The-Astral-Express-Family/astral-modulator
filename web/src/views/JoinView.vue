<script setup lang="ts">
// /join 深链兑码入伙（ADR-0009：工作区邀请链接 /join?ws=<code> 的落点）。
// 守卫已保证登录态（meta.auth=required，匿名先跳登录、成功后原路返回）。
// 带 ?ws= 自动兑换一次；无码或失败降级为手动输入。已登录用户误开
// /register 的场景由主页/侧栏引导到本能力。
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useJoinWorkspace } from '@/composables/useJoinWorkspace'
import PageHeader from '@/components/shared/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'

const route = useRoute()
const { joining, join } = useJoinWorkspace()

const code = ref(typeof route.query.ws === 'string' ? route.query.ws : '')
const autoTried = ref(false)

onMounted(() => {
  if (code.value && !autoTried.value) {
    autoTried.value = true
    void join(code.value)
  }
})

async function submit(): Promise<void> {
  await join(code.value)
}
</script>

<template>
  <div class="mx-auto flex w-full max-w-md flex-col gap-4 py-8">
    <PageHeader title="加入工作区" description="凭工作区邀请码入伙；码一次性有效。" />
    <Card>
      <CardContent>
        <form class="flex flex-col gap-4" @submit.prevent="submit">
          <FieldGroup>
            <Field>
              <FieldLabel for="join-code">邀请码</FieldLabel>
              <Input
                id="join-code"
                v-model="code"
                class="font-mono"
                placeholder="XXXXX-XXXXX-XXXXX-XXXXX"
                autocomplete="off"
              />
            </Field>
          </FieldGroup>
          <Button type="submit" :disabled="!code.trim() || joining">
            <Spinner v-if="joining" data-icon="inline-start" />
            {{ joining ? '加入中…' : '加入工作区' }}
          </Button>
        </form>
      </CardContent>
      <CardFooter>
        <p class="text-muted-foreground text-sm">
          没有邀请码？向目标工作区的管理员索取。注册邀请码不能用来入伙——
          它只用于创建账号。
        </p>
      </CardFooter>
    </Card>
  </div>
</template>
