<!-- 详情面板：查看/编辑（标题、描述、状态、优先级）、标签增删、认领/释放、
     新建子任务（v2：创建位置由父视图寻址，面板只发事件）。写操作带 expected_revision
     乐观并发；REVISION_CONFLICT 时通知父视图回源刷新，不静默覆盖。 -->
<script setup lang="ts">
import { KeyRound, Lock, Pencil, Plus, X } from '@lucide/vue'
import { computed, ref, watch } from 'vue'
import { toast } from 'vue-sonner'
import { formatApiError } from '@/api/client'
import { taskApi } from '@/api/taskSource'
import type { Actor, Tag, Task, TaskPriority, TaskStatus } from '@/api/types'
import { useSessionStore } from '@/stores/session'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { apiErrorCode, TASK_PRIORITIES, TASK_PRIORITY_META, TASK_STATUSES, TASK_STATUS_META } from './taskMeta'
import TaskStatusBadge from './TaskStatusBadge.vue'
import { fmtTime, shortId } from '../../lib/format'

const props = defineProps<{
  task: Task
  actorsById: Map<string, Actor>
  allTags: Tag[]
}>()

const emit = defineEmits<{
  /** 任何写操作成功后回传最新 Task（父视图据此更新选中详情并刷新行） */
  changed: [task: Task]
  /** 写操作撞上乐观并发：父视图负责回源 getTask 刷新 */
  stale: []
  'create-subtask': []
}>()

const session = useSessionStore()
const busy = ref(false)

const isMine = computed(() => props.task.assignee_actor_id === session.actor?.id)
const assignee = computed(() =>
  props.task.assignee_actor_id ? props.actorsById.get(props.task.assignee_actor_id) ?? null : null,
)
const availableTags = computed(() => {
  const attached = new Set(props.task.tags.map((t) => t.id))
  return props.allTags.filter((t) => !attached.has(t.id))
})

// 2.2 依赖视图（服务端恒填充）：blocked_by 非空 = 存在未完成依赖，即被阻塞。
const isBlocked = computed(() => props.task.blocked_by.length > 0)
const dependencyGroups = computed(() =>
  [
    { key: 'blocked_by', label: '被阻塞于', ids: props.task.blocked_by },
    { key: 'blocks', label: '阻塞着', ids: props.task.blocks },
    { key: 'related', label: '关联', ids: props.task.related },
  ].filter((g) => g.ids.length > 0),
)

// ---- 编辑态（切换选中任务时重置）----

const editingTitle = ref(false)
const titleDraft = ref('')
const editingDesc = ref(false)
const descDraft = ref('')

watch(
  () => props.task.id,
  () => {
    editingTitle.value = false
    editingDesc.value = false
  },
)

// ---- 统一写路径（silent：错误提示由本面板按语义发，不走全局拦截器）：
// 成功 emit changed；冲突/租约过期 emit stale + 专属文案；其余错误单行 toast。----

async function perform(action: () => Promise<Task>, successMsg: string): Promise<void> {
  busy.value = true
  try {
    const updated = await action()
    if (successMsg) toast.success(successMsg)
    emit('changed', updated)
  } catch (e) {
    if (apiErrorCode(e) === 'REVISION_CONFLICT') {
      toast.error('任务已被他人修改，已为你刷新最新内容。')
      emit('stale')
    } else {
      toast.error(formatApiError(e), { duration: 8000 })
    }
  } finally {
    busy.value = false
  }
}

function saveTitle(): void {
  const title = titleDraft.value.trim()
  if (!title) return
  void perform(
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, title }, { silent: true }),
    '标题已保存。',
  ).then(() => {
    editingTitle.value = false
  })
}

function saveDescription(): void {
  void perform(
    () =>
      taskApi.updateTask(
        props.task.id,
        {
          expected_revision: props.task.revision,
          description: descDraft.value,
        },
        { silent: true },
      ),
    '描述已保存。',
  ).then(() => {
    editingDesc.value = false
  })
}

function setStatus(status: TaskStatus): void {
  void perform(
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, status }, { silent: true }),
    `状态已改为「${TASK_STATUS_META[status]!.label}」。`,
  )
}

function setPriority(priority: TaskPriority): void {
  void perform(
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, priority }, { silent: true }),
    `优先级已改为「${TASK_PRIORITY_META[priority]!.label}」。`,
  )
}

function claim(): void {
  void perform(async () => {
    const result = await taskApi.claimTask(props.task.id, { expected_revision: props.task.revision }, { silent: true })
    return result.task
  }, '已认领，指派自己并转为进行中。')
}

function releaseClaim(): void {
  void perform(async () => {
    await taskApi.releaseClaim(props.task.id, { silent: true })
    return taskApi.getTask(props.task.id)
  }, '已释放认领。')
}

function attachTag(tagId: string): void {
  void perform(
    () => taskApi.attachTaskTag(props.task.id, tagId, props.task.revision, { silent: true }),
    '已关联标签。',
  )
}

function detachTag(tagId: string): void {
  void perform(async () => {
    await taskApi.detachTaskTag(props.task.id, tagId, { silent: true })
    return taskApi.getTask(props.task.id)
  }, '已移除标签。')
}

const actorLabel = (actor: Actor | null): string => actor?.display_name ?? '（未知成员）'
</script>

<template>
  <Card class="flex h-full flex-col">
    <CardHeader class="gap-2">
      <div class="flex items-start justify-between gap-2">
        <CardTitle class="flex min-w-0 items-center gap-2 text-base">
          <TaskStatusBadge :status="task.status" />
          <Badge v-if="isBlocked" variant="destructive">被阻塞</Badge>
          <span class="truncate">{{ task.title }}</span>
        </CardTitle>
        <Button
          v-if="!editingTitle"
          variant="ghost"
          size="icon-sm"
          aria-label="编辑标题"
          @click="
            titleDraft = task.title;
            editingTitle = true;
          "
        >
          <Pencil class="size-3.5" />
        </Button>
      </div>
      <Input
        v-if="editingTitle"
        v-model="titleDraft"
        @keydown.enter="saveTitle"
        @keydown.escape="editingTitle = false"
      />
      <div class="flex items-center gap-2">
        <Select
          :model-value="task.status"
          :disabled="busy"
          @update:model-value="setStatus($event as TaskStatus)"
        >
          <SelectTrigger size="sm" class="w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="s in TASK_STATUSES" :key="s" :value="s">
              {{ TASK_STATUS_META[s]!.label }}
            </SelectItem>
          </SelectContent>
        </Select>
        <Select
          :model-value="task.priority"
          :disabled="busy"
          @update:model-value="setPriority($event as TaskPriority)"
        >
          <SelectTrigger size="sm" class="w-24">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="p in TASK_PRIORITIES" :key="p" :value="p">
              {{ TASK_PRIORITY_META[p]!.label }}
            </SelectItem>
          </SelectContent>
        </Select>
        <span class="text-muted-foreground ml-auto font-mono text-xs">rev {{ task.revision }}</span>
      </div>
    </CardHeader>

    <CardContent class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">
      <!-- 描述 -->
      <div class="flex flex-col gap-1.5">
        <div class="flex items-center justify-between">
          <Label>描述</Label>
          <Button
            v-if="!editingDesc"
            variant="ghost"
            size="xs"
            @click="
              descDraft = task.description;
              editingDesc = true;
            "
          >
            <Pencil class="size-3" />
            编辑
          </Button>
        </div>
        <Textarea
          v-if="editingDesc"
          v-model="descDraft"
          class="min-h-24"
          placeholder="补充上下文、验收标准…"
        />
        <p v-else-if="task.description" class="text-sm whitespace-pre-wrap">{{ task.description }}</p>
        <p v-else class="text-muted-foreground text-sm">（无描述）</p>
        <div v-if="editingTitle || editingDesc" class="flex gap-2">
          <Button v-if="editingTitle" size="sm" :disabled="busy || !titleDraft.trim()" @click="saveTitle">
            保存标题
          </Button>
          <Button v-if="editingDesc" size="sm" :disabled="busy" @click="saveDescription">
            保存描述
          </Button>
          <Button variant="ghost" size="sm" @click="editingTitle = false; editingDesc = false">
            取消
          </Button>
        </div>
      </div>

      <Separator />

      <!-- 标签 -->
      <div class="flex flex-col gap-1.5">
        <Label>标签</Label>
        <div class="flex flex-wrap items-center gap-1.5">
          <Badge v-for="tag in task.tags" :key="tag.id" variant="secondary">
            {{ tag.name }}
            <X
              class="size-3 cursor-pointer opacity-60 hover:opacity-100"
              aria-label="移除标签"
              @click="detachTag(tag.id)"
            />
          </Badge>
          <DropdownMenu v-if="availableTags.length">
            <DropdownMenuTrigger as-child>
              <Button variant="outline" size="xs">
                <Plus class="size-3" />
                添加标签
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem
                v-for="tag in availableTags"
                :key="tag.id"
                @click="attachTag(tag.id)"
              >
                {{ tag.name }}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <span v-if="!task.tags.length && !availableTags.length" class="text-muted-foreground text-xs">
            （工作区暂无标签）
          </span>
        </div>
      </div>

      <Separator />

      <!-- 依赖（2.2 task_dependencies：只读展示，增删走 CLI/服务端 API） -->
      <div class="flex flex-col gap-1.5">
        <Label>依赖</Label>
        <div v-if="dependencyGroups.length" class="flex flex-col gap-1.5">
          <div v-for="group in dependencyGroups" :key="group.key" class="flex flex-wrap items-center gap-1.5">
            <span class="text-muted-foreground text-xs">{{ group.label }}</span>
            <code
              v-for="id in group.ids"
              :key="id"
              class="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
              :title="id"
            >
              {{ shortId(id) }}
            </code>
          </div>
        </div>
        <span v-else class="text-muted-foreground text-xs">（无依赖关系）</span>
      </div>

      <Separator />

      <!-- 认领 / 释放（认领 = assignee 指派给自己 + 转 in_progress，持有至释放或完成） -->
      <div class="flex flex-col gap-2">
        <Label>认领</Label>
        <div v-if="task.assignee_actor_id" class="flex items-center gap-2 rounded-lg border p-3">
          <Lock class="text-muted-foreground size-4 shrink-0" />
          <div v-if="isMine" class="flex gap-2">
            <Button size="sm" variant="outline" :disabled="busy" @click="releaseClaim">
              <KeyRound class="size-3.5" />
              释放认领
            </Button>
          </div>
          <p v-else class="text-muted-foreground text-xs">
            {{ actorLabel(assignee) }} 认领中；对方释放或由有权限者强制释放后可认领。
          </p>
        </div>
        <div v-else class="flex items-center gap-2 rounded-lg border border-dashed p-3">
          <Button size="sm" :disabled="busy" @click="claim">认领任务</Button>
          <span class="text-muted-foreground text-xs">认领 = 指派自己 + 转为进行中；持有至释放或完成</span>
        </div>
      </div>

      <Separator />

      <!-- 元信息 -->
      <div class="text-muted-foreground grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <span>负责人</span>
        <span class="text-foreground flex items-center gap-1.5">
          <template v-if="assignee">{{ actorLabel(assignee) }}</template>
          <template v-else-if="task.assignee_actor_id">
            <code class="font-mono text-xs">{{ shortId(task.assignee_actor_id) }}</code>
          </template>
          <template v-else>（未指派）</template>
        </span>
        <span>子任务</span>
        <span>{{ task.children_count > 0 ? `${task.children_count} 个` : '—' }}</span>
        <span>ID</span>
        <code class="truncate font-mono">{{ task.id }}</code>
        <span>创建</span>
        <span>{{ fmtTime(task.created_at) }}</span>
        <span>更新</span>
        <span>{{ fmtTime(task.updated_at) }}</span>
      </div>

      <Button variant="outline" size="sm" class="w-fit" :disabled="busy" @click="emit('create-subtask')">
        <Plus class="size-3.5" />
        新建子任务
      </Button>
    </CardContent>
  </Card>
</template>
