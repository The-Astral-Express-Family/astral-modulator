<script setup lang="ts">
// 密码输入框 + 小眼睛临时可见切换。包装 ui/input：透传 attrs（id/
// autocomplete/minlength/required 等）到内层 input，值走 v-model。
// 起因（2026-10-01 生产 case）：注册口令输错无法自查 → 登录失败且无自助
// 恢复。可见性切换让用户在提交前核对输入，降低同类事故。
import { ref } from 'vue'
import { EyeIcon, EyeOffIcon } from '@lucide/vue'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

defineProps<{
  modelValue?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', payload: string): void
}>()

const visible = ref(false)
const inputEl = ref<InstanceType<typeof Input> | null>(null)

// attrs（id/autocomplete/minlength/required/class…）全部透传给内层 input，
// 不落到包装 div（inheritAttrs: false），密码管理器/表单语义不受包装影响。
defineOptions({ inheritAttrs: false })

// 切换后把焦点还给输入框（连续核对场景不需要再点一次输入框）。
function toggle(): void {
  visible.value = !visible.value
  const el = (inputEl.value?.$el ?? inputEl.value) as HTMLInputElement | undefined
  el?.focus()
}
</script>

<template>
  <div class="relative w-full">
    <Input
      ref="inputEl"
      :model-value="modelValue"
      :type="visible ? 'text' : 'password'"
      :class="cn('pr-9', $attrs.class as string)"
      v-bind="$attrs"
      @update:model-value="(v: string | number) => emit('update:modelValue', String(v))"
    />
    <button
      type="button"
      :aria-label="visible ? '隐藏密码' : '显示密码'"
      :aria-pressed="visible"
      class="text-muted-foreground hover:text-foreground absolute inset-y-0 right-0 flex w-9 items-center justify-center outline-none"
      @click="toggle"
    >
      <EyeIcon v-if="visible" class="size-4" />
      <EyeOffIcon v-else class="size-4" />
    </button>
  </div>
</template>
