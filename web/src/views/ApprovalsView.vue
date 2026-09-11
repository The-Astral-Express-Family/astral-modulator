<script setup lang="ts">
// Approval 裁决队列（architecture §22 / TODO.md T-ws-6）：
// owner 在此批准/拒绝高风险动作（MVP：membership.promote_owner）。
// 待裁决列表 15s 自动刷新；裁决后立即刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { formatApiError } from '../api/client'
import { decideApproval, listApprovals } from '../api/modules/workspace'
import { useSessionStore } from '../stores/session'
import { fmtTime } from '../lib/format'
import type { Approval } from '../api/types'

const route = useRoute()
const session = useSessionStore()
const workspaceId = computed(() => route.params.workspaceId as string)

const approvals = ref<Approval[]>([])
const error = ref<string | null>(null)
const notice = ref<string | null>(null)
const busyId = ref<string | null>(null)
const loaded = ref(false)

const REFRESH_MS = 15_000
let timer: ReturnType<typeof setInterval> | null = null

async function load(): Promise<void> {
  try {
    approvals.value = (await listApprovals(workspaceId.value, { status: 'requested' })).items
    error.value = null
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    loaded.value = true
  }
}

async function decide(item: Approval, approve: boolean): Promise<void> {
  const verb = approve ? '批准并执行' : '拒绝'
  if (!window.confirm(`确定${verb}该请求（${item.action} -> ${item.target_actor_id}）？`)) return
  busyId.value = item.id
  try {
    const decided = await decideApproval(item.id, approve ? 'approve' : 'deny')
    notice.value =
      decided.status === 'executed'
        ? `已批准并执行（${item.target_actor_id} 现为 owner）。`
        : '已拒绝。'
    await load()
  } catch (e) {
    error.value = formatApiError(e)
  } finally {
    busyId.value = null
    window.setTimeout(() => (notice.value = null), 8000)
  }
}

onMounted(async () => {
  await session.boot()
  await load()
  timer = setInterval(() => {
    if (!busyId.value) void load()
  }, REFRESH_MS)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <h2>裁决队列</h2>
  <div class="card">
    <p class="muted">workspace: <code>{{ workspaceId }}</code>（每 15 秒自动刷新）</p>
    <p v-if="error" class="error-text">{{ error }}</p>
    <p v-if="notice" class="notice-text">{{ notice }}</p>

    <p v-if="loaded && !approvals.length && !error" class="muted">没有待裁决的请求。</p>
    <table v-if="approvals.length" style="width: 100%; border-collapse: collapse">
      <thead>
        <tr>
          <th style="text-align: left">动作</th>
          <th style="text-align: left">目标</th>
          <th style="text-align: left">发起人</th>
          <th style="text-align: left">过期时间</th>
          <th style="text-align: left">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in approvals" :key="item.id">
          <td><code>{{ item.action }}</code></td>
          <td><code>{{ item.target_actor_id }}</code></td>
          <td><code>{{ item.requested_by }}</code></td>
          <td>{{ fmtTime(item.expires_at) }}</td>
          <td>
            <button :disabled="busyId === item.id" @click="decide(item, true)">批准</button>
            <button :disabled="busyId === item.id" style="margin-left: 8px" @click="decide(item, false)">
              拒绝
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
