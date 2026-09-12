<script setup lang="ts">
// 待裁决列表表格：动作 / 目标 / 发起人 / 过期时间 / 操作。
// 裁决只 emit，确认弹窗与请求由父视图（ApprovalsView）处理。
import type { Approval } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

defineProps<{
  items: Approval[]
  busyId: string | null
}>()

const emit = defineEmits<{
  decide: [id: string, decision: 'approve' | 'deny']
}>()
</script>

<template>
  <Table>
    <TableHeader>
      <TableRow>
        <TableHead>动作</TableHead>
        <TableHead>目标</TableHead>
        <TableHead>发起人</TableHead>
        <TableHead>过期时间</TableHead>
        <TableHead>操作</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      <TableRow v-for="item in items" :key="item.id">
        <TableCell>
          <code class="font-mono text-xs">{{ item.action }}</code>
        </TableCell>
        <TableCell>
          <code class="font-mono text-xs">{{ item.target_actor_id }}</code>
        </TableCell>
        <TableCell>
          <code class="font-mono text-xs">{{ item.requested_by }}</code>
        </TableCell>
        <TableCell>{{ fmtTime(item.expires_at) }}</TableCell>
        <TableCell>
          <div class="flex gap-2">
            <Button
              size="sm"
              :disabled="busyId === item.id"
              @click="emit('decide', item.id, 'approve')"
            >
              批准
            </Button>
            <Button
              size="sm"
              variant="destructive"
              :disabled="busyId === item.id"
              @click="emit('decide', item.id, 'deny')"
            >
              拒绝
            </Button>
          </div>
        </TableCell>
      </TableRow>
    </TableBody>
  </Table>
</template>
