<!-- workspace 切换器：列表来自 workspaces store（与创建对话框共享，
     新建后即时可见）；当前值跟随路由（深链/页面间切换都正确高亮）；
     切换保持当前子页（工作区 id 是路径前缀，/a/tasks → /b/tasks），
     无工作区上下文的页面落到新 workspace 概览。深链命中列表外
     workspace 时补一次详情取名。 -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { FolderOpenIcon } from '@lucide/vue'
import { getWorkspace } from '@/api/modules/core'
import type { Workspace } from '@/api/types'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select'
import { useWorkspacesStore } from '@/stores/workspaces'

const route = useRoute()
const router = useRouter()
const store = useWorkspacesStore()

// 深链直达列表外的 workspace（分页截断/刚被拉进协作）时补拉的名字。
const extra = ref<Workspace | null>(null)

const currentId = computed(() =>
  typeof route.params.workspaceId === 'string' ? route.params.workspaceId : '',
)

onMounted(() => {
  void store.load()
})

watch(
  [currentId, () => store.loaded] as const,
  async ([id, isLoaded]) => {
    if (!isLoaded || !id) return
    if (store.items.some((w) => w.id === id)) return
    if (extra.value?.id === id) return
    try {
      extra.value = await getWorkspace(id)
    } catch {
      extra.value = null
    }
  },
  { immediate: true },
)

const current = computed<Workspace | null>(() => {
  if (!currentId.value) return null
  return (
    store.items.find((w) => w.id === currentId.value) ??
    (extra.value?.id === currentId.value ? extra.value : null)
  )
})

// 工作区子页的路由名 → 路径后缀映射（与 router/index.ts 的 children 一一对应）。
// 切换工作区时按当前路由名取后缀，页面类别不因切换丢失。
const SUB_PATH_BY_NAME: Record<string, string> = {
  'workspace-overview': '',
  'workspace-tasks': '/tasks',
  'workspace-approvals': '/approvals',
  'workspace-tags': '/tags',
  'workspace-messages': '/messages',
  'workspace-documents': '/documents',
  'workspace-conflicts': '/conflicts',
  'workspace-members': '/members',
  'workspace-audit': '/audit',
}

function onSelect(value: unknown): void {
  const id = String(value)
  if (!id || id === currentId.value) return
  const suffix = SUB_PATH_BY_NAME[typeof route.name === 'string' ? route.name : ''] ?? ''
  void router.push(`/workspaces/${id}${suffix}`)
}
</script>

<template>
  <Select :model-value="currentId || undefined" @update:model-value="onSelect">
    <SelectTrigger class="w-full" aria-label="切换工作区">
      <FolderOpenIcon class="size-4 shrink-0 text-muted-foreground" />
      <span class="min-w-0 flex-1 truncate text-left">
        {{ current?.name ?? '选择工作区' }}
      </span>
    </SelectTrigger>
    <SelectContent>
      <SelectItem v-for="w in store.items" :key="w.id" :value="w.id">
        {{ w.name }}
      </SelectItem>
    </SelectContent>
  </Select>
</template>
