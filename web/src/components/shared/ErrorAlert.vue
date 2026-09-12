<!-- 破坏性警示：API / 引导错误的统一展示。 -->
<script setup lang="ts">
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

defineProps<{
  message: string
  title?: string
}>()
</script>

<template>
  <Alert variant="destructive">
    <AlertTitle v-if="title">{{ title }}</AlertTitle>
    <AlertDescription>{{ message }}</AlertDescription>
  </Alert>
</template>
