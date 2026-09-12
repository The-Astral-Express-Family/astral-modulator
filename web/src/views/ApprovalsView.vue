<script setup lang="ts">
// Approval 裁决队列（architecture §22 / TODO.md T-ws-6）：
// owner 在此批准/拒绝高风险动作（MVP：membership.promote_owner）。
// 待裁决列表 15s 自动刷新；裁决后立即刷新。
import { computed, onMounted, ref } from 'vue'
import { toast } from 'vue-sonner'
import { decideApproval, listApprovals } from '@/api/modules/workspace'
import type { Approval } from '@/api/types'
import { useApiAction } from '@/composables/useApiAction'
import { usePolling } from '@/composables/usePolling'
import { useWorkspaceId } from '@/composables/useWorkspaceId'
import ApprovalsTable from '@/components/approvals/ApprovalsTable.vue'
import ErrorAlert from '@/components/shared/ErrorAlert.vue'
import PageHeader from '@/components/shared/PageHeader.vue'
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
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'

const workspaceId = useWorkspaceId()
const { error, run } = useApiAction()

const approvals = ref<Approval[]>([])
const busyId = ref<string | null>(null)
const loaded = ref(false)

// 待确认的裁决（window.confirm 的替代：AlertDialog 异步流）。
const pending = ref<{ item: Approval; decision: 'approve' | 'deny' } | null>(null)
const dialogOpen = ref(false)

const REFRESH_MS = 15_000
const { start } = usePolling(load, REFRESH_MS, { skip: () => busyId.value !== null })

async function load(): Promise<void> {
  await run(async () => {
    approvals.value = (await listApprovals(workspaceId.value, { status: 'requested' })).items
  })
  loaded.value = true
}

function requestDecision(id: string, decision: 'approve' | 'deny'): void {
  const item = approvals.value.find((candidate) => candidate.id === id)
  if (!item) return
  pending.value = { item, decision }
  dialogOpen.value = true
}

function confirmVerb(): string {
  return pending.value?.decision === 'approve' ? '批准并执行' : '拒绝'
}

const confirmText = computed(
  () =>
    `确定${confirmVerb()}该请求（${pending.value?.item.action ?? ''} -> ${pending.value?.item.target_actor_id ?? ''}）？`,
)

async function confirmDecision(): Promise<void> {
  const target = pending.value
  if (!target) return
  dialogOpen.value = false
  busyId.value = target.item.id
  const ok = await run(async () => {
    const decided = await decideApproval(target.item.id, target.decision)
    toast.success(
      decided.status === 'executed'
        ? `已批准并执行（${target.item.target_actor_id} 现为 owner）。`
        : '已拒绝。',
      { duration: 8000 },
    )
  })
  if (ok) await load()
  busyId.value = null
}

onMounted(async () => {
  await load()
  start()
})
</script>

<template>
  <div class="flex w-full flex-col gap-4">
    <PageHeader title="裁决队列" />
    <p class="text-muted-foreground text-sm">
      workspace: <code class="font-mono text-xs">{{ workspaceId }}</code>（每 15 秒自动刷新）
    </p>

    <ErrorAlert v-if="error" :message="error" />

    <Empty v-if="loaded && !approvals.length && !error" class="border">
      <EmptyHeader>
        <EmptyTitle>没有待裁决的请求。</EmptyTitle>
      </EmptyHeader>
    </Empty>

    <Card v-if="approvals.length">
      <CardContent>
        <ApprovalsTable :items="approvals" :busy-id="busyId" @decide="requestDecision" />
      </CardContent>
    </Card>

    <AlertDialog :open="dialogOpen" @update:open="dialogOpen = $event">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ confirmVerb() }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ confirmText }}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" @click="confirmDecision">
            {{ confirmVerb() }}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
