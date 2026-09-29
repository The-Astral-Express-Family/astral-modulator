<!-- 加入工作区对话框：凭工作区邀请码入伙（ADR-0009）。API 调用内置
     （侧栏场景，父组件只管开关），与 CreateWorkspaceDialog 同构；
     成功后刷新共享列表并跳转该工作区概览。 -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useJoinWorkspace } from '@/composables/useJoinWorkspace'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const { joining, join } = useJoinWorkspace()
const code = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open) return
    code.value = ''
  },
)

async function submit(): Promise<void> {
  if (!code.value.trim() || joining.value) return
  if (await join(code.value)) emit('update:open', false)
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>加入工作区</DialogTitle>
        <DialogDescription>
          输入工作区管理员发给你的邀请码。码一次性有效，入伙后即失效。
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-1.5">
        <Label for="join-code">邀请码</Label>
        <Input
          id="join-code"
          v-model="code"
          class="font-mono"
          placeholder="XXXXX-XXXXX-XXXXX-XXXXX"
          autocomplete="off"
          @keydown.enter="submit"
        />
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">取消</Button>
        <Button :disabled="!code.trim() || joining" @click="submit">
          {{ joining ? '加入中…' : '加入' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
