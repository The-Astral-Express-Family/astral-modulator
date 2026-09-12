<script setup lang="ts">
// 设备授权查找卡片：输入 CLI 显示的 user_code 并发起查询。
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

const props = defineProps<{
  modelValue: string
  busy: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  lookup: [code: string]
}>()

const code = computed({
  get: () => props.modelValue,
  set: (value: string | number) => emit('update:modelValue', String(value)),
})

function submit(): void {
  emit('lookup', code.value)
}
</script>

<template>
  <Card>
    <CardContent>
      <FieldGroup>
        <Field>
          <FieldLabel for="device-user-code">输入 CLI 显示的代码：</FieldLabel>
          <Input
            id="device-user-code"
            v-model="code"
            class="w-40"
            placeholder="XXXX-XXXX"
            :disabled="busy"
            @keyup.enter="submit"
          />
        </Field>
      </FieldGroup>
    </CardContent>
    <CardFooter>
      <Button :disabled="busy" @click="submit">查询</Button>
    </CardFooter>
  </Card>
</template>
