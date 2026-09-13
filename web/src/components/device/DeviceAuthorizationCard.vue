<script setup lang="ts">
// 设备授权详情卡片：user_code 大号展示 + 批准/拒绝操作。
// 拒绝走 AlertDialog 二次确认（异步流：确认后回调 emit）。
import { ref } from 'vue'
import type { DeviceAuthorizationView } from '@/api/modules/auth'
import KeyValue from '@/components/shared/KeyValue.vue'
import StatusBadge from '@/components/shared/StatusBadge.vue'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

defineProps<{
  view: DeviceAuthorizationView
  busy: boolean
}>()

const emit = defineEmits<{
  approve: []
  deny: []
}>()

const confirmOpen = ref(false)

function onConfirmDeny(): void {
  confirmOpen.value = false
  emit('deny')
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>CLI 请求登录</CardTitle>
      <CardDescription>
        一个 CLI 客户端正在请求登录此账号。请核对代码与终端显示完全一致后批准；
        如非你本人操作，请拒绝。
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="flex flex-col gap-1">
        <span class="text-muted-foreground text-sm">请求代码</span>
        <span class="font-mono text-2xl tracking-widest">{{ view.user_code }}</span>
      </div>
      <KeyValue label="客户端类型" :value="view.client_type" />
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground text-sm">状态</span>
        <StatusBadge :status="view.status" />
      </div>
    </CardContent>
    <CardFooter class="gap-2">
      <Button :disabled="busy" @click="emit('approve')">批准</Button>
      <Button variant="destructive" :disabled="busy" @click="confirmOpen = true">拒绝</Button>
    </CardFooter>
  </Card>

  <AlertDialog :open="confirmOpen" @update:open="confirmOpen = $event">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>确认拒绝该设备授权请求？</AlertDialogTitle>
        <AlertDialogDescription>
          拒绝后该设备授权请求将终止，CLI 将无法通过该代码完成登录。
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>取消</AlertDialogCancel>
        <AlertDialogAction variant="destructive" @click="onConfirmDeny">拒绝</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
