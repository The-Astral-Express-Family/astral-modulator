<!-- 文档编辑面板（DocumentsView 详情卡的编辑态）：
     - 职责：编辑 ⇄ 预览切换、脏检查、保存（乐观并发 base_revision/base_hash）、
       409 冲突引导（跳冲突裁决视图，不自制 merge UI）、1MiB 客户端预检；
     - 编辑器：MarkdownEditor（CodeMirror 6，惰性加载，token 化主题）；
       预览复用 MarkdownDoc（渲染管线与查看态完全一致）；
     - 保存语义：成功后 emit('saved', doc) 交父级刷新；409/400/超限错误就
       地展示（错误条 + formatApiError），不全局 toast（apiFetch 非 silent
       已会 toast，双通道不重复：编辑面板用 silent 调用 + 本地错误条）。 -->
<script setup lang="ts">
import { computed, markRaw, ref, shallowRef, watch, type Component } from 'vue'
import { EyeIcon, PencilIcon, SaveIcon, TriangleAlertIcon, UndoIcon } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import MarkdownDoc from '@/components/shared/MarkdownDoc.vue'
import LoadSwap from '@/components/shared/LoadSwap.vue'
import { formatApiError } from '@/api/client'
import { pushDocument } from '@/api/modules/documents'
import type { DocumentDto } from '@/api/modules/documents'

const props = defineProps<{
  workspaceId: string
  path: string
  /** 进入编辑态时加载的基线（base_revision / base_hash / content）。 */
  base: DocumentDto
  /** tombstone 恢复：保存走 revive 分支（base_revision=0）。 */
  revive?: boolean
}>()

const emit = defineEmits<{
  (e: 'saved', doc: DocumentDto): void
  (e: 'cancel'): void
  (e: 'conflict'): void
  (e: 'dirty-change', dirty: boolean): void
}>()

// 编辑器惰性加载：仅首次进入编辑态拉取 CM6 chunk（~90KB gz）。类型仅取
// 组件本身（typeof import 会连带要求 props 匹配 defineExpose，放宽为
// Component 以免双向约束聚型失败）。
const MarkdownEditor = shallowRef<Component | null>(null)
void import('@/components/shared/MarkdownEditor.vue').then((m) => {
  MarkdownEditor.value = markRaw(m.default)
})

const draft = ref(props.base.content)
const previewing = ref(false)
const saving = ref(false)
const errorText = ref('')
const conflicted = ref(false)
const idempotencyKey = ref(crypto.randomUUID())

const dirty = computed(() => draft.value !== props.base.content)
watch(dirty, (d) => emit('dirty-change', d), { immediate: true })
const effectiveBase = computed(() =>
  props.revive ? 0 : props.base.revision,
)

// 切换文档（父级复用面板换 path/base）时重置草稿与错误态。
watch(
  () => [props.path, props.base.revision, props.base.content_hash] as const,
  () => {
    draft.value = props.base.content
    errorText.value = ''
    conflicted.value = false
    previewing.value = false
    idempotencyKey.value = crypto.randomUUID()
  },
)

const MAX_BYTES = 1024 * 1024

async function save(): Promise<void> {
  if (saving.value || !dirty.value || conflicted.value) return
  const bytes = new TextEncoder().encode(draft.value).length
  if (bytes > MAX_BYTES) {
    errorText.value = `内容 ${bytes} 字节，超出 1MiB 上限，无法保存。`
    return
  }
  saving.value = true
  errorText.value = ''
  try {
    const ZERO_HASH = `sha256:${'0'.repeat(64)}`
    const doc = await pushDocument(
      props.workspaceId,
      props.path,
      {
        base_revision: effectiveBase.value,
        base_hash: props.revive ? ZERO_HASH : props.base.content_hash,
        content: draft.value,
      },
      { silent: true, idempotencyKey: idempotencyKey.value },
    )
    idempotencyKey.value = crypto.randomUUID()
    emit('saved', doc)
  } catch (e) {
    const msg = formatApiError(e)
    errorText.value = msg
    if (/DOCUMENT_CONFLICT/.test(msg)) {
      conflicted.value = true
      emit('conflict')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="border-input flex gap-0.5 rounded-lg border p-0.5">
        <Button
          :variant="!previewing ? 'default' : 'ghost'"
          size="xs"
          :disabled="saving"
          @click="previewing = false"
        >
          <PencilIcon class="size-3" />
          编辑
        </Button>
        <Button
          :variant="previewing ? 'default' : 'ghost'"
          size="xs"
          :disabled="saving"
          @click="previewing = true"
        >
          <EyeIcon class="size-3" />
          预览
        </Button>
      </div>
      <span class="text-muted-foreground text-xs">
        {{ dirty ? '未保存' : '无变更' }} · 基线 r{{ base.revision }}
      </span>
      <span class="grow" />
      <Button variant="outline" size="xs" :disabled="saving" @click="emit('cancel')">
        <UndoIcon class="size-3" />
        放弃
      </Button>
      <Button size="xs" :disabled="!dirty || saving || conflicted" @click="save">
        <SaveIcon class="size-3" />
        {{ saving ? '保存中…' : '保存' }}
      </Button>
    </div>

    <p
      v-if="errorText"
      class="border-destructive/40 bg-destructive/10 text-destructive flex items-start gap-2 rounded-lg border px-3 py-2 text-xs"
    >
      <TriangleAlertIcon class="mt-0.5 size-3.5 shrink-0" />
      <span>
        {{ errorText }}
        <template v-if="conflicted">
          他人已先保存（你的基线 r{{ base.revision }} 已过期）。请复制本地修改后，
          <a class="underline underline-offset-2" @click.prevent="emit('conflict')">
            前往冲突裁决
          </a>
          完成合并；或「放弃」回到查看态。
        </template>
      </span>
    </p>

    <div v-show="previewing">
      <MarkdownDoc :content="draft" />
    </div>
    <!-- 编辑器 chunk 惰性加载：骨架期占位与编辑器同高（min-h-[60vh]），
         到位时经 LoadSwap 交叉淡换，高度零跳动。v-show 保证预览态只剩渲染视图。 -->
    <LoadSwap :loading="!MarkdownEditor" class="min-h-[60vh]">
      <template #skeleton>
        <div class="flex flex-col gap-2">
          <Skeleton class="h-5 w-1/2" />
          <Skeleton class="h-56 w-full" />
        </div>
      </template>
      <template #default>
        <div v-show="!previewing" class="flex min-h-[60vh] flex-col">
          <component
            :is="MarkdownEditor"
            v-model="draft"
            @save="save"
          />
        </div>
      </template>
    </LoadSwap>
  </div>
</template>
