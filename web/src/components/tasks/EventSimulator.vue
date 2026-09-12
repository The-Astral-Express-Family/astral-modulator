<!-- 事件模拟器（仅 mock 模式渲染）：把 task.* / tag.* / snapshot.required 事件
     注入内存事件总线，演示实时更新路径。动作基于当前选中任务（无选中则按钮禁用）。 -->
<script setup lang="ts">
import type { Tag } from '@/api/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  simulateCreateTask,
  simulateDeleteTag,
  simulateLeaseExpire,
  simulateOtherClaim,
  simulateOtherRelease,
  simulateOtherUpdate,
  simulateRenameTag,
  simulateSnapshotRequired,
} from '@/mocks/taskMock'
import { toast } from 'vue-sonner'
import { computed } from 'vue'

const props = defineProps<{
  open: boolean
  selectedTaskId: string | null
  tags: Tag[]
}>()

const emit = defineEmits<{ 'update:open': [value: boolean] }>()

const needsSelection = computed(() => !props.selectedTaskId)
const randomTag = (): Tag | undefined =>
  props.tags.length ? props.tags[Math.floor(Math.random() * props.tags.length)] : undefined

function run(label: string, action: () => unknown, requireTask = true): void {
  if (requireTask && needsSelection.value) return
  try {
    void action()
    toast.info(`模拟器：已触发「${label}」。`)
  } catch (e) {
    toast.error(`模拟器失败：${String(e)}`)
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>事件模拟器</DialogTitle>
        <DialogDescription>
          注入 SSE 同形事件驱动视图实时更新（真实部署中由服务端 outbox 投递）。
          部分动作需要先在左侧选中一个任务。
        </DialogDescription>
      </DialogHeader>

      <div class="grid grid-cols-2 gap-2">
        <Button variant="outline" size="sm" :disabled="needsSelection" @click="run('他人认领', () => simulateOtherClaim(selectedTaskId!))">
          他人认领
        </Button>
        <Button variant="outline" size="sm" :disabled="needsSelection" @click="run('他人释放', () => simulateOtherRelease(selectedTaskId!))">
          他人释放
        </Button>
        <Button variant="outline" size="sm" :disabled="needsSelection" @click="run('租约立即过期', () => simulateLeaseExpire(selectedTaskId!))">
          租约立即过期
        </Button>
        <Button variant="outline" size="sm" :disabled="needsSelection" @click="run('他人修改标题', () => simulateOtherUpdate(selectedTaskId!))">
          他人修改标题
        </Button>
        <Button variant="outline" size="sm" :disabled="needsSelection" @click="run('新建子任务', () => simulateCreateTask(selectedTaskId))">
          新建子任务事件
        </Button>
        <Button variant="outline" size="sm" @click="run('新建根任务', () => simulateCreateTask(null), false)">
          新建根任务事件
        </Button>
        <Button
          variant="outline"
          size="sm"
          :disabled="!tags.length"
          @click="run('重命名标签', () => simulateRenameTag(randomTag()!.id), false)"
        >
          重命名一个标签
        </Button>
        <Button
          variant="outline"
          size="sm"
          :disabled="!tags.length"
          @click="run('删除标签', () => simulateDeleteTag(randomTag()!.id), false)"
        >
          删除一个标签
        </Button>
        <Button
          variant="destructive"
          size="sm"
          class="col-span-2"
          @click="run('快照过期', () => simulateSnapshotRequired(), false)"
        >
          snapshot.required（游标超窗 → 全量重载）
        </Button>
      </div>
    </DialogContent>
  </Dialog>
</template>
