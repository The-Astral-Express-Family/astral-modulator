<!-- 新建任务对话框（v2）：创建位置由父视图寻址（workspace 容器 = 根任务，
     task 容器 = 子任务），对话框只收字段。v2 的 TaskCreate 支持按名附带
     已存在标签，故表单提供标签多选。 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { TaskPriority } from '@/api/types'
import type { TaskCreatePayload } from '@/api/modules/task'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { TASK_PRIORITIES, TASK_PRIORITY_META } from './taskMeta'

const props = defineProps<{
  open: boolean
  tags: { id: string; name: string }[]
  /** 创建位置提示：null = 根任务，否则为容器任务标题 */
  parentTitle: string | null
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  submit: [payload: TaskCreatePayload]
}>()

const title = ref('')
const description = ref('')
const priority = ref<TaskPriority>('normal')
const selectedTagNames = ref<string[]>([])

watch(
  () => props.open,
  (open) => {
    if (!open) return
    title.value = ''
    description.value = ''
    priority.value = 'normal'
    selectedTagNames.value = []
  },
)

const titleValid = computed(() => title.value.trim().length > 0 && title.value.length <= 500)

function toggleTag(name: string, checked: boolean): void {
  selectedTagNames.value = checked
    ? [...selectedTagNames.value, name]
    : selectedTagNames.value.filter((n) => n !== name)
}

function submit(): void {
  if (!titleValid.value) return
  emit('submit', {
    title: title.value.trim(),
    description: description.value,
    priority: priority.value,
    ...(selectedTagNames.value.length ? { tags: selectedTagNames.value } : {}),
  })
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>新建任务</DialogTitle>
        <DialogDescription>
          创建位置：{{
            parentTitle ? `「${parentTitle}」的子任务` : '根任务（workspace 容器）'
          }}。创建后状态为「待办」，可从详情面板认领。
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-1.5">
          <Label for="task-title">标题</Label>
          <Input
            id="task-title"
            v-model="title"
            placeholder="任务标题（1–500 字符）"
            @keydown.enter="submit"
          />
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5">
            <Label>优先级</Label>
            <Select v-model="priority">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="优先级" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="p in TASK_PRIORITIES" :key="p" :value="p">
                  {{ TASK_PRIORITY_META[p]!.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>标签（可选）</Label>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="outline" class="w-full justify-start font-normal">
                  {{
                    selectedTagNames.length
                      ? selectedTagNames.join('、')
                      : '选择已存在的标签'
                  }}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" class="w-44">
                <DropdownMenuCheckboxItem
                  v-for="t in tags"
                  :key="t.id"
                  :model-value="selectedTagNames.includes(t.name)"
                  @update:model-value="toggleTag(t.name, $event === true)"
                  @select.prevent
                >
                  {{ t.name }}
                </DropdownMenuCheckboxItem>
                <p v-if="!tags.length" class="text-muted-foreground px-2 py-1.5 text-xs">
                  （工作区暂无标签）
                </p>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <Label for="task-desc">描述</Label>
          <Textarea id="task-desc" v-model="description" placeholder="补充上下文、验收标准…" class="min-h-24" />
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">取消</Button>
        <Button :disabled="!titleValid" @click="submit">创建</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
