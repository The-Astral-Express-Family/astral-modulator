<!-- 新建工作区对话框：API 调用内置（侧栏场景，父组件只管开关）。
     成功后刷新共享列表并跳转新工作区概览；失败留在对话框重试
     （409 重名/400 slug 不可派生等由全局 toast 呈现）。 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { createWorkspace } from '@/api/modules/core'
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
import { useWorkspacesStore } from '@/stores/workspaces'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const router = useRouter()
const workspaces = useWorkspacesStore()

const name = ref('')
const slug = ref('')
const submitting = ref(false)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    name.value = ''
    slug.value = ''
    submitting.value = false
  },
)

const nameValid = computed(() => name.value.trim().length > 0 && name.value.length <= 200)

async function submit(): Promise<void> {
  if (!nameValid.value || submitting.value) return
  submitting.value = true
  try {
    const ws = await createWorkspace({
      name: name.value.trim(),
      ...(slug.value.trim() ? { slug: slug.value.trim() } : {}),
    })
    toast.success(`已创建工作区「${ws.name}」，你是最初的 owner。`)
    emit('update:open', false)
    void workspaces.load(true)
    await router.push(`/workspaces/${ws.id}`)
  } catch {
    // 全局 toast 已提示（409 重名 / 400 slug 不可派生等）；留在对话框重试。
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>新建工作区</DialogTitle>
        <DialogDescription>
          创建后你自动成为该工作区的 owner，并切换到其概览页。
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-1.5">
          <Label for="ws-name">名称</Label>
          <Input
            id="ws-name"
            v-model="name"
            placeholder="工作区名称（1–200 字符）"
            @keydown.enter="submit"
          />
        </div>

        <div class="flex flex-col gap-1.5">
          <Label for="ws-slug">Slug（可选）</Label>
          <Input
            id="ws-slug"
            v-model="slug"
            placeholder="留空则由名称自动生成"
            @keydown.enter="submit"
          />
          <p class="text-muted-foreground text-xs">
            仅小写字母、数字与连字符；名称含中文等非 ASCII 字符时无法自动生成，需手动提供。
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">取消</Button>
        <Button :disabled="!nameValid || submitting" @click="submit">
          {{ submitting ? '创建中…' : '创建' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
