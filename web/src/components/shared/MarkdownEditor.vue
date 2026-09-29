<!-- CodeMirror 6 Markdown 编辑器（文档编辑态专用）：
     - 主题手写 token 化：全部取 shadcn 语义 token（--muted / --foreground /
       --primary …），暗色模式经 .dark 自动适配，零独立暗色分支；
     - CM6 打包为惰性 chunk：本组件由 DocumentsView 动态 import，非编辑态
       零成本（编辑器仅编辑时才需要）；
     - 语义化接口：v-model 内容、save 快捷键（Ctrl/Cmd+S）、readonly 预览态。
     CM 高亮依赖 @lezer/highlight 的语义 tag → class 映射，styleTags 不引
     任何内置主题包（one-dark 等），保证 token 化的一致性。 -->
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { EditorState, Compartment, type Extension } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { tags as t } from '@lezer/highlight'

const props = withDefaults(
  defineProps<{
    modelValue: string
    /** 预览态：编辑器只读化（仍可滚动选中文本），保存快捷键禁用。 */
    readonly?: boolean
    placeholder?: string
  }>(),
  { readonly: false, placeholder: '' },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'save'): void
}>()

const host = ref<HTMLDivElement>()

// readonly 走 compartment：切换不重建 state，undo 历史与光标全保留。
const readonlyComp = new Compartment()

// ---- token 化主题 ----

// 语法高亮：lezer tag → 语义 token class，颜色在 <style> 里以 var() 落地。
const mdHighlight = HighlightStyle.define([
  { tag: t.heading, class: 'cm-md-heading' },
  { tag: t.heading1, class: 'cm-md-h1' },
  { tag: t.heading2, class: 'cm-md-h2' },
  { tag: t.heading3, class: 'cm-md-h3' },
  { tag: t.emphasis, class: 'cm-md-em' },
  { tag: t.strong, class: 'cm-md-strong' },
  { tag: t.link, class: 'cm-md-link' },
  { tag: t.url, class: 'cm-md-url' },
  { tag: t.monospace, class: 'cm-md-code' },
  { tag: [t.processingInstruction, t.meta], class: 'cm-md-meta' },
  { tag: t.quote, class: 'cm-md-quote' },
  { tag: t.list, class: 'cm-md-list' },
  { tag: t.strikethrough, class: 'cm-md-strike' },
  { tag: t.contentSeparator, class: 'cm-md-sep' },
])

// 编辑器底色/边框/字体的最小主题（EditorView.theme 经 CSS 变量取色）。
const editorTheme = EditorView.theme({
  '&': { height: '100%', fontSize: '0.8125rem' },
  '.cm-scroller': {
    fontFamily: 'var(--font-mono)',
    lineHeight: '1.7',
  },
  '.cm-content': { padding: '0.75rem 1rem' },
  '.cm-gutters': {
    backgroundColor: 'transparent',
    color: 'var(--muted-foreground)',
    border: 'none',
  },
  '.cm-activeLine': { backgroundColor: 'color-mix(in oklab, var(--muted) 45%, transparent)' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent' },
  '.cm-selectionBackground': {
    backgroundColor: 'color-mix(in oklab, var(--primary) 22%, transparent)',
  },
  '&.cm-focused': { outline: 'none' },
  '.cm-cursor': { borderLeftColor: 'var(--primary)' },
})

// ---- 实例装配 ----

let view: EditorView | null = null

function buildExtensions(): Extension[] {
  return [
    history(),
    keymap.of([
      {
        key: 'Mod-s',
        run: () => {
          if (!props.readonly) emit('save')
          return true
        },
      },
      ...defaultKeymap,
      ...historyKeymap,
    ]),
    markdown({ base: markdownLanguage, addKeymap: false }),
    syntaxHighlighting(mdHighlight),
    editorTheme,
    EditorView.lineWrapping,
    readonlyComp.of(EditorState.readOnly.of(props.readonly)),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
    }),
  ]
}

onMounted(() => {
  mount()
})

function mount(): void {
  if (!host.value || view) return
  view = new EditorView({
    state: EditorState.create({ doc: props.modelValue, extensions: buildExtensions() }),
    parent: host.value,
  })
}

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

// 外部内容变更（如重新拉取、切换文档）→ 同步进编辑器；避免回写死循环，
// 仅在文本确实不同才 dispatch。
watch(
  () => props.modelValue,
  (next) => {
    if (!view) return
    const cur = view.state.doc.toString()
    if (next === cur) return
    view.dispatch({
      changes: { from: 0, to: cur.length, insert: next },
    })
  },
)

// readonly 切换（编辑 ⇄ 预览）经 compartment 重配：state/undo 历史保留。
watch(
  () => props.readonly,
  (ro) => {
    view?.dispatch({ effects: readonlyComp.reconfigure(EditorState.readOnly.of(ro)) })
  },
)

defineExpose({ focus: () => view?.focus() })
</script>

<template>
  <div
    ref="host"
    class="border-input focus-within:border-ring focus-within:ring-ring/30 rounded-lg border focus-within:ring-2"
  />
</template>

<style>
/* CM6 注入的 DOM 不带 scoped 属性，样式走全局命名空间（cm-md-* 仅本组件使用）。
   颜色一律取语义 token：暗色模式由 .dark 覆写 token 自动生效。 */
.cm-md-heading {
  font-weight: 600;
  line-height: 1.3;
}
.cm-md-h1 { font-size: 1.35em; }
.cm-md-h2 { font-size: 1.2em; }
.cm-md-h3 { font-size: 1.08em; }
.cm-md-em { font-style: italic; }
.cm-md-strong { font-weight: 700; }
.cm-md-link { color: var(--primary); text-decoration: underline; text-underline-offset: 2px; }
.cm-md-url { color: var(--muted-foreground); }
.cm-md-code {
  color: var(--primary);
  background: var(--muted);
  border-radius: 4px;
  padding: 0.08em 0.3em;
}
.cm-md-meta { color: var(--muted-foreground); }
.cm-md-quote { color: var(--muted-foreground); font-style: italic; }
.cm-md-list { color: var(--primary); }
.cm-md-strike { text-decoration: line-through; color: var(--muted-foreground); }
.cm-md-sep { color: var(--border); }
</style>
