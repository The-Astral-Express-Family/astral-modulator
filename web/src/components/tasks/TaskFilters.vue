<!-- 工具栏（v2 语义）：regex/fuzzy/assignee 三个搜索输入 + 状态/标签两个服务端过滤。
     状态/标签变更即时生效（重载可见集合）；regex/fuzzy/assignee 由「查询」按钮显式触发
     task-search；「返回树」清空搜索条件回到树模式。 -->
<script setup lang="ts">
import { Search } from '@lucide/vue'
import type { Tag } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { fromSelectValue, SELECT_ALL, toSelectValue } from '@/lib/selectAll'
import { TASK_STATUSES, TASK_STATUS_META } from './taskMeta'

const props = defineProps<{
  fuzzy: string
  regex: string
  assignee: string
  status: string
  tag: string
  tags: Tag[]
}>()

const emit = defineEmits<{
  'update:fuzzy': [value: string]
  'update:regex': [value: string]
  'update:assignee': [value: string]
  'update:status': [value: string]
  'update:tag': [value: string]
  search: []
  reset: []
}>()
</script>

<template>
  <div class="flex flex-wrap items-end gap-2">
    <div class="relative min-w-48 flex-1">
      <Label class="text-muted-foreground mb-1 block text-xs">模糊（按相关度排序）</Label>
      <div class="relative">
        <Search class="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
        <Input
          :model-value="fuzzy"
          placeholder="关键词…"
          class="pl-8"
          @update:model-value="emit('update:fuzzy', String($event))"
          @keydown.enter="emit('search')"
        />
      </div>
    </div>

    <div class="min-w-48 flex-1">
      <Label class="text-muted-foreground mb-1 block text-xs">正则（过滤标题/描述）</Label>
      <Input
        :model-value="regex"
        placeholder="regex…"
        @update:model-value="emit('update:regex', String($event))"
        @keydown.enter="emit('search')"
      />
    </div>

    <div class="w-36">
      <Label class="text-muted-foreground mb-1 block text-xs">负责人</Label>
      <Input
        :model-value="assignee"
        placeholder="actor id（可选）"
        @update:model-value="emit('update:assignee', String($event))"
        @keydown.enter="emit('search')"
      />
    </div>

    <div class="w-32">
      <Label class="text-muted-foreground mb-1 block text-xs">状态</Label>
      <Select
        :model-value="toSelectValue(status)"
        @update:model-value="emit('update:status', fromSelectValue($event))"
      >
        <SelectTrigger class="w-full">
          <SelectValue placeholder="全部状态" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem :value="SELECT_ALL">全部状态</SelectItem>
          <SelectItem v-for="s in TASK_STATUSES" :key="s" :value="s">
            {{ TASK_STATUS_META[s]!.label }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>

    <div class="w-32">
      <Label class="text-muted-foreground mb-1 block text-xs">标签</Label>
      <Select
        :model-value="toSelectValue(tag)"
        @update:model-value="emit('update:tag', fromSelectValue($event))"
      >
        <SelectTrigger class="w-full">
          <SelectValue placeholder="全部标签" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem :value="SELECT_ALL">全部标签</SelectItem>
          <SelectItem v-for="t in tags" :key="t.id" :value="t.name">{{ t.name }}</SelectItem>
        </SelectContent>
      </Select>
    </div>

    <div class="flex gap-2">
      <Button size="sm" @click="emit('search')">查询</Button>
      <Button size="sm" variant="outline" @click="emit('reset')">返回树</Button>
    </div>
  </div>
</template>
