<!-- 文档目录树（受管清单的树形视图，DocumentsView 侧栏）：
     - manifest 是扁平 path 列表，目录由路径前缀在客户端合成（目录本身不是
       服务端实体，无 manifest 行）；排序目录优先、自然序（doc2 < doc10）；
     - memory/ 保留子空间（M1 裁决）整枝特殊标注：目录用 BrainIcon +
       「记忆」徽章，枝内文件图标着色（violet），与非记忆文件区分；
     - 右键菜单（shadcn context-menu）：文件=查看/编辑/复制路径/重命名/移动/
       删除，tombstone 文件=查看/恢复/复制路径，目录=新建文档/复制路径/
       重命名/移动/删除（目录操作为批量语义，由父级实现）；
     - 原生 HTML5 拖拽：文件与目录可拖到目录节点（或根区）完成移动，目录
       不允许拖进自身子树；tombstone 不可拖（移动需先恢复）。
       拖拽语义事件 move-drop(fromPath, fromIsDir, toDir) 上抛，父级校验落点
       并执行（目录拖放只预填对话框，由用户确认）；
     - 展开态为会话级 Set：首列目录默认展开，选中文件时自动展开其祖先。 -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  BrainIcon,
  ChevronRightIcon,
  CopyIcon,
  EyeIcon,
  FilePlusIcon,
  FileTextIcon,
  FolderIcon,
  FolderInputIcon,
  FolderOpenIcon,
  PencilIcon,
  PenLineIcon,
  RotateCcwIcon,
  Trash2Icon,
} from '@lucide/vue'
import { toast } from 'vue-sonner'
import type { ManifestItemDto } from '@/api/modules/documents'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import { Badge } from '@/components/ui/badge'

const props = defineProps<{
  /** 已过滤的 manifest 行（memoryOnly / include_deleted 由父级决定） */
  items: ManifestItemDto[]
  selectedPath: string
}>()

const emit = defineEmits<{
  (e: 'select', path: string): void
  (e: 'edit', path: string): void
  (e: 'restore', path: string): void
  (e: 'rename', path: string, isDir: boolean): void
  (e: 'move', path: string, isDir: boolean): void
  (e: 'remove', path: string, isDir: boolean): void
  (e: 'create-in', dir: string): void
  (e: 'move-drop', fromPath: string, fromIsDir: boolean, toDir: string): void
}>()

// ---- 树构建（客户端合成目录）----

interface DocTreeNode {
  type: 'dir' | 'file'
  name: string
  /** 目录 path 不带尾斜杠；文件为完整 manifest path */
  path: string
  children: DocTreeNode[]
  doc?: ManifestItemDto
  deleted: boolean
  /** memory/ 保留子空间（目录自身或其祖先命中） */
  memory: boolean
}

function parentOf(path: string): string {
  const i = path.lastIndexOf('/')
  return i < 0 ? '' : path.slice(0, i)
}

function buildTree(list: ManifestItemDto[]): DocTreeNode[] {
  const dirs = new Map<string, DocTreeNode>()
  const root: DocTreeNode[] = []
  const ensureDir = (path: string): DocTreeNode => {
    const hit = dirs.get(path)
    if (hit) return hit
    const node: DocTreeNode = {
      type: 'dir',
      name: path.split('/').pop() ?? path,
      path,
      children: [],
      deleted: false,
      memory: path === 'memory' || path.startsWith('memory/'),
    }
    dirs.set(path, node)
    const parent = parentOf(path)
    if (parent) ensureDir(parent).children.push(node)
    else root.push(node)
    return node
  }
  for (const it of list) {
    const node: DocTreeNode = {
      type: 'file',
      name: it.path.split('/').pop() ?? it.path,
      path: it.path,
      children: [],
      doc: it,
      deleted: it.deleted === true,
      memory: it.path.startsWith('memory/'),
    }
    const parent = parentOf(it.path)
    if (parent) ensureDir(parent).children.push(node)
    else root.push(node)
  }
  const sortRec = (nodes: DocTreeNode[]): void => {
    nodes.sort((a, b) =>
      a.type !== b.type
        ? a.type === 'dir'
          ? -1
          : 1
        : a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }),
    )
    for (const n of nodes) if (n.type === 'dir') sortRec(n.children)
  }
  sortRec(root)
  return root
}

const tree = computed(() => buildTree(props.items))

// ---- 展开态（会话级；首列目录默认展开 + 选中即展开祖先）----

const expanded = ref(new Set<string>())
let seeded = false

watch(
  tree,
  (t) => {
    if (seeded || !t.length) return
    seeded = true
    for (const n of t) if (n.type === 'dir') expanded.value.add(n.path)
  },
  { immediate: true },
)

watch(
  () => props.selectedPath,
  (p) => {
    if (!p) return
    const segs = p.split('/')
    segs.pop()
    let cur = ''
    for (const s of segs) {
      cur = cur ? `${cur}/${s}` : s
      expanded.value.add(cur)
    }
  },
  { immediate: true },
)

function toggleDir(node: DocTreeNode): void {
  if (expanded.value.has(node.path)) expanded.value.delete(node.path)
  else expanded.value.add(node.path)
}

interface TreeRow {
  node: DocTreeNode
  depth: number
}

const rows = computed<TreeRow[]>(() => {
  const out: TreeRow[] = []
  const walk = (nodes: DocTreeNode[], depth: number): void => {
    for (const n of nodes) {
      out.push({ node: n, depth })
      if (n.type === 'dir' && expanded.value.has(n.path)) walk(n.children, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

// ---- 拖拽移动（原生 HTML5 DnD；跨组件数据走 ref，dataTransfer 仅作载体）----

const dragFrom = ref<{ path: string; isDir: boolean } | null>(null)
const dragOverDir = ref<string | null>(null) // '' = 根区
const dragOverRow = ref<string | null>(null) // 当前悬停行（高亮判据）

function draggable(node: DocTreeNode): boolean {
  return node.type === 'dir' || !node.deleted
}

function dropAllowed(targetDir: string): boolean {
  if (!dragFrom.value) return false
  const from = dragFrom.value.path
  if (from === targetDir) return false
  if (dragFrom.value.isDir && targetDir.startsWith(`${from}/`)) return false // 拖进自身子树
  if (parentOf(from) === targetDir) return false // 同目录内无位移
  return true
}

function onDragStart(e: DragEvent, node: DocTreeNode): void {
  if (!draggable(node)) {
    e.preventDefault()
    return
  }
  dragFrom.value = { path: node.path, isDir: node.type === 'dir' }
  e.dataTransfer?.setData('text/plain', node.path)
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragEnd(): void {
  dragFrom.value = null
  dragOverDir.value = null
  dragOverRow.value = null
}

// 行级：目录行落自身目录，文件行落其父目录（「放到文件旁 = 同目录」）。
function onDragOver(e: DragEvent, node: DocTreeNode): void {
  const targetDir = dropTargetDir(node)
  if (!dropAllowed(targetDir)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  dragOverDir.value = targetDir
  dragOverRow.value = node.path
}

function onDrop(e: DragEvent, node: DocTreeNode): void {
  const targetDir = dropTargetDir(node)
  if (!dragFrom.value || !dropAllowed(targetDir)) return
  e.preventDefault()
  emit('move-drop', dragFrom.value.path, dragFrom.value.isDir, targetDir)
  onDragEnd()
}

// 根区（容器空白处）作为落点 = 移到根目录。
function onContainerDragOver(e: DragEvent): void {
  if (!dropAllowed('')) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  dragOverDir.value = ''
  dragOverRow.value = null
}

function onContainerDrop(e: DragEvent): void {
  if (!dragFrom.value || !dropAllowed('')) return
  e.preventDefault()
  emit('move-drop', dragFrom.value.path, dragFrom.value.isDir, '')
  onDragEnd()
}

function onDragLeave(e: DragEvent): void {
  if (dragOverDir.value !== '') return
  const to = e.relatedTarget
  if (to instanceof Node && e.currentTarget instanceof Node && e.currentTarget.contains(to)) return
  dragOverDir.value = null
}

// 行级拖放目标目录：目录行落自身，文件行落其父目录（「放到文件旁 = 同目录」）。
function dropTargetDir(node: DocTreeNode): string {
  return node.type === 'dir' ? node.path : parentOf(node.path)
}

// ---- 右键菜单 ----

function copyPath(path: string): void {
  void navigator.clipboard
    .writeText(path)
    .then(() => toast.success(`路径已复制：${path}`))
    .catch(() => toast.error('复制失败：剪贴板不可用'))
}

const isDirOpen = (node: DocTreeNode): boolean =>
  node.type === 'dir' && expanded.value.has(node.path)

const rowSelected = (node: DocTreeNode): boolean =>
  node.type === 'file' && props.selectedPath === node.path
</script>

<template>
  <div
    class="flex flex-col"
    :class="dragFrom && dragOverDir === '' ? 'bg-primary/5 ring-primary/40 rounded-lg ring-1 ring-dashed' : ''"
    @dragover="onContainerDragOver"
    @dragleave="onDragLeave"
    @drop="onContainerDrop"
  >
    <template v-for="row in rows" :key="`${row.node.type}:${row.node.path}`">
      <ContextMenu>
        <ContextMenuTrigger as-child>
          <button
            type="button"
            :draggable="draggable(row.node)"
            class="hover:bg-muted/40 flex w-full items-center gap-1.5 rounded-lg pr-2 text-left text-sm"
            :class="[
              rowSelected(row.node) ? 'bg-primary/10' : '',
              dragOverRow === row.node.path ? 'bg-primary/5 ring-primary/40 ring-1' : '',
              row.node.deleted ? 'opacity-55' : '',
            ]"
            :style="{ paddingLeft: `${0.375 + row.depth * 0.875}rem` }"
            :title="row.node.path"
            @click="row.node.type === 'dir' ? toggleDir(row.node) : emit('select', row.node.path)"
            @dblclick="row.node.type === 'file' && !row.node.deleted && emit('edit', row.node.path)"
            @dragstart="onDragStart($event, row.node)"
            @dragend="onDragEnd"
            @dragover.stop="onDragOver($event, row.node)"
            @drop.stop="onDrop($event, row.node)"
          >
            <template v-if="row.node.type === 'dir'">
              <ChevronRightIcon
                class="text-muted-foreground size-3.5 shrink-0 transition-transform"
                :class="isDirOpen(row.node) ? 'rotate-90' : ''"
              />
              <BrainIcon
                v-if="row.node.memory"
                class="size-4 shrink-0 text-violet-500 dark:text-violet-400"
              />
              <FolderOpenIcon
                v-else-if="isDirOpen(row.node)"
                class="text-muted-foreground size-4 shrink-0"
              />
              <FolderIcon v-else class="text-muted-foreground size-4 shrink-0" />
            </template>
            <template v-else>
              <span class="size-3.5 shrink-0" />
              <FileTextIcon
                class="size-4 shrink-0"
                :class="row.node.memory ? 'text-violet-500/80 dark:text-violet-400/80' : 'text-muted-foreground'"
              />
            </template>

            <span
              class="min-w-0 flex-1 truncate"
              :class="row.node.deleted ? 'line-through decoration-destructive/60' : ''"
            >
              {{ row.node.name }}
            </span>

            <Badge v-if="row.node.type === 'dir' && row.node.memory" variant="secondary" class="shrink-0">
              <BrainIcon class="size-3" />
              记忆
            </Badge>
            <Badge v-if="row.node.deleted" variant="destructive" class="shrink-0">已删除</Badge>
            <span
              v-if="row.node.type === 'file' && row.node.doc"
              class="text-muted-foreground hidden shrink-0 font-mono text-[10px] sm:inline"
            >
              r{{ row.node.doc.revision }}
            </span>
          </button>
        </ContextMenuTrigger>

        <!-- 文件（tombstone 只保留 查看/恢复/复制路径；rename/move/delete 需要活行） -->
        <ContextMenuContent class="w-44">
          <template v-if="row.node.type === 'file'">
            <ContextMenuItem @select="emit('select', row.node.path)">
              <EyeIcon />
              查看
            </ContextMenuItem>
            <template v-if="!row.node.deleted">
              <ContextMenuItem @select="emit('edit', row.node.path)">
                <PencilIcon />
                编辑
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem @select="copyPath(row.node.path)">
                <CopyIcon />
                复制路径
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem @select="emit('rename', row.node.path, false)">
                <PenLineIcon />
                重命名…
              </ContextMenuItem>
              <ContextMenuItem @select="emit('move', row.node.path, false)">
                <FolderInputIcon />
                移动到…
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem variant="destructive" @select="emit('remove', row.node.path, false)">
                <Trash2Icon />
                删除…
              </ContextMenuItem>
            </template>
            <template v-else>
              <ContextMenuItem @select="emit('restore', row.node.path)">
                <RotateCcwIcon />
                恢复…
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem @select="copyPath(row.node.path)">
                <CopyIcon />
                复制路径
              </ContextMenuItem>
            </template>
          </template>

          <!-- 目录：新建/复制路径/重命名/移动/删除（后三者为批量，父级实现） -->
          <template v-else>
            <ContextMenuItem @select="emit('create-in', row.node.path)">
              <FilePlusIcon />
              新建文档
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem @select="copyPath(row.node.path)">
              <CopyIcon />
              复制路径
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem @select="emit('rename', row.node.path, true)">
              <PenLineIcon />
              重命名…
            </ContextMenuItem>
            <ContextMenuItem @select="emit('move', row.node.path, true)">
              <FolderInputIcon />
              移动到…
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem variant="destructive" @select="emit('remove', row.node.path, true)">
              <Trash2Icon />
              删除…
            </ContextMenuItem>
          </template>
        </ContextMenuContent>
      </ContextMenu>
    </template>
  </div>
</template>
