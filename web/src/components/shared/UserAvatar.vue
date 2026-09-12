<!-- 用户头像：avatar_url 外链优先（加载失败自动回退），否则显示
     用户名首字符 + 按 actor id 确定性生成的底色（同一用户恒定）。 -->
<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { computed } from 'vue'
import type { Actor } from '@/api/types'
import type { AvatarVariants } from '@/components/ui/avatar'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'

const props = defineProps<{
  actor: Pick<Actor, 'id' | 'display_name' | 'avatar_url'>
  size?: AvatarVariants['size']
  /** 回退字符的附加 class（放大展示时调字号，如 text-2xl） */
  fallbackClass?: HTMLAttributes['class']
}>()

const initial = computed(() => {
  const name = props.actor.display_name.trim()
  return name ? (Array.from(name)[0] ?? '?').toUpperCase() : '?'
})

// id 哈希 → 色相；饱和度/亮度固定在可读范围，避免与主题冲突的花色。
const fallbackBg = computed(() => {
  let hue = 0
  for (const ch of props.actor.id) hue = (hue * 31 + (ch.codePointAt(0) ?? 0)) % 360
  return `hsl(${hue} 45% 42%)`
})
</script>

<template>
  <Avatar :size="size">
    <AvatarImage v-if="actor.avatar_url" :src="actor.avatar_url" :alt="actor.display_name" />
    <AvatarFallback :style="{ backgroundColor: fallbackBg }" :class="fallbackClass" class="font-medium text-white">
      {{ initial }}
    </AvatarFallback>
  </Avatar>
</template>
