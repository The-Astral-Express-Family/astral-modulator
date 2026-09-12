<script setup lang="ts">
// 能力卡片：服务端 capabilities（最低 CLI 版本 + feature 列表）。
import type { Capabilities } from '@/api/types'
import JsonBlock from '@/components/shared/JsonBlock.vue'
import KeyValue from '@/components/shared/KeyValue.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

defineProps<{
  capabilities: Capabilities
}>()
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>能力</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-2">
      <KeyValue label="minimum_cli_version" :value="capabilities.minimum_cli_version" />
      <JsonBlock :value="capabilities.features" />
    </CardContent>
  </Card>
</template>
