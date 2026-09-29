<!-- 受管 Markdown 文档的渲染视图（可选，默认渲染）：
     - marked 解析 + DOMPurify 清洗（XSS 防线），二者均动态 import 懒加载，
       首次渲染才付出 ~30KB gz 成本；
     - 渲染 / 源码切换（segmented 按钮，同「含已删除 / 仅看记忆」款式），
       偏好经 localStorage 记忆，默认渲染；
     - GFM 表格 / 任务列表 / 删划线由 .markdown-body 样式承接（手写 token 化，
       见下方 <style>，不引 typography 插件）。 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { FileCodeIcon, FileTextIcon } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

const props = defineProps<{ content: string }>()

// ---- 视图偏好（渲染 / 源码），localStorage 记忆，默认渲染 ----

type ViewMode = 'rendered' | 'source'
const PREF_KEY = 'astral.doc.view-mode'

function readPref(): ViewMode {
  try {
    return localStorage.getItem(PREF_KEY) === 'source' ? 'source' : 'rendered'
  } catch {
    return 'rendered'
  }
}

const viewMode = ref<ViewMode>(readPref())

function setMode(mode: ViewMode): void {
  viewMode.value = mode
  try {
    localStorage.setItem(PREF_KEY, mode)
  } catch {
    // 隐私模式等存储不可用：仅本次会话内生效，不报错。
  }
}

// ---- 懒加载渲染管线 ----

type MarkedModule = typeof import('marked')
type PurifyModule = typeof import('dompurify')

let markedModule: MarkedModule | null = null
let purifyModule: PurifyModule | null = null

const html = ref('')
const rendering = ref(false)
let renderToken = 0

async function loadParsers(): Promise<{
  marked: MarkedModule['marked']
  purify: PurifyModule['default']
}> {
  if (!markedModule || !purifyModule) {
    // 动态 import：首次渲染才拉取解析器；模块级缓存后续零网络成本。
    ;[markedModule, purifyModule] = await Promise.all([import('marked'), import('dompurify')])
  }
  return { marked: markedModule.marked, purify: purifyModule.default }
}

async function render(): Promise<void> {
  const token = ++renderToken
  rendering.value = true
  try {
    const { marked, purify } = await loadParsers()
    // gfm：表格 / 任务列表 / 删划线；breaks 关闭——md 段内换行不应变 <br>。
    marked.setOptions({ gfm: true, breaks: false })
    const raw = marked.parse(props.content) as string
    if (token !== renderToken) return
    html.value = purify.sanitize(raw, {
      // 默认允许集本就含 input（GFM 任务列表 checkbox 需要，事件处理器
      // 已被默认剥离）；显式禁 style/form/button 收紧攻击面。
      FORBID_TAGS: ['style', 'form', 'button'],
      FORBID_ATTR: ['style'],
    })
  } catch {
    // 渲染失败不致命：回退源码视图，内容仍可读。
    if (token === renderToken) {
      html.value = ''
      viewMode.value = 'source'
    }
  } finally {
    if (token === renderToken) rendering.value = false
  }
}

const showRendered = computed(() => viewMode.value === 'rendered')

watch(
  () => [props.content, viewMode.value] as const,
  () => {
    if (viewMode.value === 'rendered') void render()
    else html.value = ''
  },
  { immediate: true },
)
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="border-input flex gap-0.5 rounded-lg border p-0.5">
        <Button
          :variant="viewMode === 'rendered' ? 'default' : 'ghost'"
          size="xs"
          @click="setMode('rendered')"
        >
          <FileTextIcon class="size-3" />
          渲染
        </Button>
        <Button
          :variant="viewMode === 'source' ? 'default' : 'ghost'"
          size="xs"
          @click="setMode('source')"
        >
          <FileCodeIcon class="size-3" />
          源码
        </Button>
      </div>
      <span v-if="rendering" class="text-muted-foreground text-xs">渲染中…</span>
    </div>

    <div v-if="rendering" class="flex flex-col gap-2">
      <Skeleton class="h-5 w-1/2" />
      <Skeleton class="h-24 w-full" />
      <Skeleton class="h-5 w-2/3" />
    </div>
    <!-- 内容经 DOMPurify 清洗后插入（v-html 的唯一受控使用点）。
         animate-in fade-in：内容首次挂载淡入（元素跨内容更新复用，不会逐键
         重放动画），替换骨架时不硬蹦。 -->
    <div
      v-else-if="showRendered && html"
      class="markdown-body animate-in fade-in duration-300"
      v-html="html"
    />
    <pre
      v-else
      class="bg-muted/40 max-h-[70vh] animate-in fade-in duration-300 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap break-all"
      >{{ content }}</pre
    >
  </div>
</template>

<style>
/* .markdown-body：手写 token 化 md 排版。v-html 注入的内容不带 scoped
   data 属性，scoped + :deep() 的块状写法在 Tailwind v4（Lightning CSS）
   下也不可靠，故用非 scoped 样式 + .markdown-body 命名空间隔离——该类
   仅本组件使用。深色模式由语义 token 自动适配，无需独立暗色分支。 */
.markdown-body {
  & > :first-child {
    margin-top: 0;
  }
  & > :last-child {
    margin-bottom: 0;
  }

  h1,
  h2,
  h3,
  h4,
  h5,
  h6 {
    font-weight: 600;
    line-height: 1.3;
    margin: 1.5em 0 0.6em;
  }
  h1 {
    border-bottom: 1px solid var(--border);
    font-size: 1.5em;
    padding-bottom: 0.3em;
  }
  h2 {
    border-bottom: 1px solid var(--border);
    font-size: 1.3em;
    padding-bottom: 0.25em;
  }
  h3 {
    font-size: 1.15em;
  }
  h4,
  h5,
  h6 {
    font-size: 1em;
  }

  p {
    margin: 0.75em 0;
  }

  a {
    color: var(--primary);
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  ul,
  ol {
    list-style: revert;
    margin: 0.75em 0;
    padding-left: 1.5em;
  }
  ul {
    list-style-type: disc;
  }
  ol {
    list-style-type: decimal;
  }
  li {
    margin: 0.25em 0;
  }
  li > ul,
  li > ol {
    margin: 0.25em 0;
  }
  /* GFM 任务列表：marked 不输出 task-list 类名，以首个 checkbox 子元素定位；
     去圆点并对齐 checkbox。 */
  li:has(> input[type='checkbox']) {
    list-style-type: none;
    margin-left: -1.5em;
    padding-left: 0;
  }
  li > input[type='checkbox'] {
    margin-right: 0.45em;
    vertical-align: middle;
  }

  blockquote {
    border-left: 3px solid var(--border);
    color: var(--muted-foreground);
    margin: 0.75em 0;
    padding: 0.1em 1em;
  }

  code {
    background: var(--muted);
    border-radius: 4px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.875em;
    padding: 0.15em 0.4em;
  }
  pre {
    background: var(--muted);
    border: 1px solid var(--border);
    border-radius: 8px;
    margin: 0.75em 0;
    max-height: 28rem;
    overflow: auto;
    padding: 0.75em 1em;
  }
  pre code {
    background: none;
    border: none;
    font-size: 0.8125rem;
    padding: 0;
    white-space: pre;
  }

  table {
    border-collapse: collapse;
    display: block;
    margin: 0.75em 0;
    max-width: 100%;
    overflow-x: auto;
  }
  th,
  td {
    border: 1px solid var(--border);
    padding: 0.4em 0.75em;
    text-align: left;
  }
  th {
    background: var(--muted);
    font-weight: 600;
  }

  hr {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 1.5em 0;
  }

  img {
    border-radius: 8px;
    max-width: 100%;
  }
}
</style>
