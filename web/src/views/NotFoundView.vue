<script setup lang="ts">
// 404 页：无逻辑，仅 Empty + 返回入口。
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { FileQuestionIcon } from '@lucide/vue'
</script>

<template>
  <Empty class="mx-auto max-w-md">
    <EmptyMedia variant="icon">
      <FileQuestionIcon />
    </EmptyMedia>
    <EmptyHeader>
      <EmptyTitle>404</EmptyTitle>
      <EmptyDescription>页面不存在。</EmptyDescription>
    </EmptyHeader>
    <EmptyContent>
      <Button as-child>
        <RouterLink to="/">返回总览</RouterLink>
      </Button>
    </EmptyContent>
  </Empty>
</template>
