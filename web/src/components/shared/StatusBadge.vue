<!-- 状态徽章：设备授权 / 审批状态词 → Badge variant 映射，未知状态回退 secondary，展示原文。 -->
<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'

const props = defineProps<{
  status: string
}>()

const STATUS_VARIANTS: Record<string, NonNullable<BadgeVariants['variant']>> = {
  pending: 'secondary',
  requested: 'secondary',
  approved: 'default',
  executed: 'default',
  denied: 'destructive',
  rejected: 'destructive',
  expired: 'outline',
}

const variant = computed(() => STATUS_VARIANTS[props.status] ?? 'secondary')
</script>

<template>
  <Badge :variant="variant">{{ status }}</Badge>
</template>
