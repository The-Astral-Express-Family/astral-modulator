<!-- 详情面板：查看/编辑（标题、描述、状态、优先级）、标签增删、认领/续租/释放、
     新建子任务（v2：创建位置由父视图寻址，面板只发事件）。写操作带 expected_revision
     乐观并发；REVISION_CONFLICT 时通知父视图回源刷新，不静默覆盖。 -->
<script setup lang="ts">
import { KeyRound, Lock, Pencil, Plus, RotateCcw, X } from '@lucide/vue'
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
import TaskLeaseBadge from './TaskLeaseBadge.vue'
import TaskStatusBadge from './TaskStatusBadge.vue'

const props = defineProps<{
  task: Task
  actorsById: Map<string, Actor>
  allTags: Tag[]
}>()

const emit = defineEmits<{
  /** 任何写操作成功后回传最新 Task（父视图据此更新选中详情并刷新行） */
  changed: [task: Task]
  /** 写操作撞上乐观并发/租约过期：父视图负责回源 getTask 刷新 */
  stale: []
  'create-subtask': []
}>()

const session = useSessionStore()
const busy = ref(false)

const isMyLease = computed(() => props.task.lease?.holder_actor_id === session.actor?.id)
const assignee = computed(() =>
  props.task.assignee_actor_id ? props.actorsById.get(props.task.assignee_actor_id) ?? null : null,
)
const leaseHolder = computed(() =>
  props.task.lease ? props.actorsById.get(props.task.lease.holder_actor_id) ?? null : null,
)
const availableTags = computed(() => {
  const attached = new Set(props.task.tags.map((t) => t.id))
  return props.allTags.filter((t) => !attached.has(t.id))
})

// ---- 编辑态（切换选中任务时重置）----

const editingTitle = ref(false)
const titleDraft = ref('')
const editingDesc = ref(false)
const descDraft = ref('')
const claimSeconds = ref('300')

watch(
  () => props.task.id,
  () => {
    editingTitle.value = false
    editingDesc.value = false
  },
)

// ---- 统一写路径：成功 emit changed；冲突/租约过期 emit stale；其余错误 toast ----

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
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, title }),
    '标题已保存。',
  ).then(() => {
    editingTitle.value = false
  })
}

function saveDescription(): void {
  void perform(
    () =>
      taskApi.updateTask(props.task.id, {
        expected_revision: props.task.revision,
        description: descDraft.value,
      }),
    '描述已保存。',
  ).then(() => {
    editingDesc.value = false
  })
}

function setStatus(status: TaskStatus): void {
  void perform(
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, status }),
    `状态已改为「${TASK_STATUS_META[status]!.label}」。`,
  )
}

function setPriority(priority: TaskPriority): void {
  void perform(
    () => taskApi.updateTask(props.task.id, { expected_revision: props.task.revision, priority }),
    `优先级已改为「${TASK_PRIORITY_META[priority]!.label}」。`,
  )
}

function claim(): void {
  void perform(async () => {
    const result = await taskApi.claimTask(props.task.id, {
      expected_revision: props.task.revision,
      lease_seconds: Number(claimSeconds.value),
    })
    return result.task
  }, '已认领，租约生效。')
}

function renewLease(): void {
  busy.value = true
  taskApi
    .renewLease(props.task.id)
    .then((lease) => {
      toast.success('租约已续期。')
      emit('changed', { ...props.task, lease })
    })
    .catch((e: unknown) => {
      if (apiErrorCode(e) === 'TASK_LEASE_EXPIRED') {
        toast.error('租约已过期，需重新认领。')
        emit('stale')
      } else {
        toast.error(formatApiError(e), { duration: 8000 })
      }
    })
    .finally(() => {
      busy.value = false
    })
}

function releaseLease(): void {
  void perform(async () => {
    await taskApi.releaseLease(props.task.id)
    return taskApi.getTask(props.task.id)
  }, '已释放租约。')
}

function attachTag(tagId: string): void {
  void perform(
    () => taskApi.attachTaskTag(props.task.id, tagId, props.task.revision),
    '已关联标签。',
  )
}

function detachTag(tagId: string): void {
  void perform(async () => {
    await taskApi.detachTaskTag(props.task.id, tagId)
    return taskApi.getTask(props.task.id)
  }, '已移除标签。')
}

const fmtTime = (iso: string): string => new Date(iso).toLocaleString()
const actorLabel = (actor: Actor | null): string => actor?.display_name ?? '（未知成员）'
const shortId = (id: string): string => id.slice(0, 12) + '…'
</script>

<template>
  <Card class="flex h-full flex-col">
    <CardHeader class="gap-2">
      <div class="flex items-start justify-between gap-2">
        <CardTitle class="flex min-w-0 items-center gap-2 text-base">
          <TaskStatusBadge :status="task.status" />
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

      <!-- 租约 / 认领 -->
      <div class="flex flex-col gap-2">
        <Label>租约</Label>
        <div v-if="task.lease" class="flex flex-col gap-2 rounded-lg border p-3">
          <div class="flex items-center gap-2 text-sm">
            <Lock class="text-muted-foreground size-4 shrink-0" />
            <TaskLeaseBadge :lease="task.lease" :is-mine="isMyLease" />
            <span class="text-muted-foreground text-xs">
              持有者：{{ actorLabel(leaseHolder) }}
            </span>
          </div>
          <div v-if="isMyLease" class="flex gap-2">
            <Button size="sm" variant="outline" :disabled="busy" @click="renewLease">
              <RotateCcw class="size-3.5" />
              续租 5 分钟
            </Button>
            <Button size="sm" variant="outline" :disabled="busy" @click="releaseLease">
              <KeyRound class="size-3.5" />
              释放
            </Button>
          </div>
          <p v-else class="text-muted-foreground text-xs">
            他人持有中，租约到期或释放后可认领。
          </p>
        </div>
        <div v-else class="flex items-center gap-2 rounded-lg border border-dashed p-3">
          <Select v-model="claimSeconds">
            <SelectTrigger size="sm" class="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="300">5 分钟</SelectItem>
              <SelectItem value="900">15 分钟</SelectItem>
              <SelectItem value="1800">30 分钟</SelectItem>
              <SelectItem value="3600">60 分钟</SelectItem>
            </SelectContent>
          </Select>
          <Button size="sm" :disabled="busy" @click="claim">认领任务</Button>
          <span class="text-muted-foreground text-xs">认领 = 加租约 + 指派自己 + 转为进行中</span>
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
