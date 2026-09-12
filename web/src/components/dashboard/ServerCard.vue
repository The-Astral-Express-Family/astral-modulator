<script setup lang="ts">
// 服务器信息卡片：/.well-known/astral 元数据（server_id / canonical_url / protocol_version）。
import type { WellKnown } from '@/api/types'
import KeyValue from '@/components/shared/KeyValue.vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

defineProps<{
  wellKnown: WellKnown
}>()
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>服务器</CardTitle>
      <CardDescription>server_id（稳定身份，CLI 绑定主键）</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-2">
      <KeyValue label="server_id" :value="wellKnown.server_id" />
      <KeyValue label="canonical_url" :value="wellKnown.canonical_url" />
      <KeyValue label="protocol_version" :value="wellKnown.protocol_version" />
    </CardContent>
  </Card>
</template>
