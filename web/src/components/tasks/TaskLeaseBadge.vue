<!-- 租约徽章：锁图标 + 每秒倒计时；悬浮显示持有者语义与到期绝对时间。 -->
<script setup lang="ts">
import { computed, onScopeDispose, ref } from 'vue'
import { Lock } from '@lucide/vue'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { Lease } from '@/api/types'

const props = defineProps<{ lease: Lease; isMine: boolean }>()

// 每实例一秒心跳（展示中的实例数=打开的详情/少量行内徽章，可接受）。
const now = ref(Date.now())
const timer = setInterval(() => {
  now.value = Date.now()
}, 1000)
onScopeDispose(() => clearInterval(timer))

const msLeft = computed(() => new Date(props.lease.expires_at).getTime() - now.value)

const countdown = computed(() => {
  const s = Math.max(0, Math.floor(msLeft.value / 1000))
  const m = Math.floor(s / 60)
  if (m >= 60) return `${Math.floor(m / 60)}h ${m % 60}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
})

const tone = computed(() => {
  if (props.isMine) return 'text-emerald-600 dark:text-emerald-400'
  return 'text-amber-600 dark:text-amber-400'
})
</script>

<template>
  <TooltipProvider :delay-duration="200">
    <Tooltip>
      <TooltipTrigger as-child>
        <span class="inline-flex items-center gap-1 whitespace-nowrap text-xs" :class="tone">
          <Lock class="size-3.5 shrink-0" />
          {{ countdown }}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <p>{{ isMine ? '我持有的租约' : '被他人认领' }} · 至 {{ new Date(lease.expires_at).toLocaleTimeString() }}</p>
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
