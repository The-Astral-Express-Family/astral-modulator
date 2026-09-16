// 游标分页列表状态机（G4 收敛：六视图 9 处同构「加载更多」形态的单一实现）。
// 本层只管 items / cursor / loaded / loading 与「拉一页 → append 合并 → 推进
// next_cursor」循环；fetcher 由调用方注入，silent 与错误呈现是调用方语义：
// - 全局 toast 形态（缺省）：fetcher 按调用透传 silent，失败由 api/client
//   拦截器 toast，error ref 无人消费；
// - 内联错误形态（AuditView）：fetcher 恒 silent，error ref 接住原始错误由
//   视图格式化呈现（成功即清空，避免 toast 双弹）。
// 注意：TaskTreeView 的树/搜索分页形态不同（容器化 + 自动续拉 + SSE 回拉
// 保量），不适用本状态机，勿强行迁移。

import { ref } from 'vue'
import type { Ref } from 'vue'

/** 各 API 模块分页信封的公共形状（生成类型 items / next_cursor 均可选）。 */
export interface CursorPage<T> {
  items?: T[]
  next_cursor?: string | null
}

/** fetcher 第二参：调用方 load 时传入的 silent 原样透传（是否消费由 fetcher 决定）。 */
export interface CursorFetchOpts {
  silent?: boolean
}

export interface CursorLoadOpts {
  append?: boolean
  silent?: boolean
}

export type CursorFetcher<T> = (
  cursor: string | null,
  opts: CursorFetchOpts,
) => Promise<CursorPage<T>>

export interface UseCursorListResult<T> {
  items: Ref<T[]>
  /** 下一页游标；null = 已到末页。过滤重查前可由调用方置 null 防失败残留。 */
  cursor: Ref<string | null>
  /** 首次加载已结束（无论成败）——空态展示判据。 */
  loaded: Ref<boolean>
  /** 任一拉取进行中（首页或加载更多共用——按钮/骨架判据）。 */
  loading: Ref<boolean>
  /** 追加拉取进行中（loading 的子集，供需要区分首页/续拉的调用方）。 */
  loadingMore: Ref<boolean>
  /** 最近一次失败的原始错误（成功清空）；toast 形态可忽略。 */
  error: Ref<unknown>
  load(opts?: CursorLoadOpts): Promise<void>
  loadMore(): Promise<void>
  reset(): void
}

export function useCursorList<T>(fetcher: CursorFetcher<T>): UseCursorListResult<T> {
  const items = ref<T[]>([]) as Ref<T[]>
  const cursor = ref<string | null>(null)
  const loaded = ref(false)
  const loading = ref(false)
  const loadingMore = ref(false)
  const error = ref<unknown>(null)

  async function load(opts: CursorLoadOpts = {}): Promise<void> {
    const append = opts.append === true
    loading.value = true
    if (append) loadingMore.value = true
    try {
      // 非追加（首页/过滤重查）不给游标；追加时给当前游标（fetcher 收到
      // null 自行省略参数）。失败不动 items/cursor——保留已加载内容与停转圈。
      const page = await fetcher(append ? cursor.value : null, { silent: opts.silent })
      const fresh = page.items ?? [] // 生成类型 items 可选（allOf 合并/内联形态）
      items.value = append ? [...items.value, ...fresh] : fresh
      cursor.value = page.next_cursor ?? null
      error.value = null
    } catch (e) {
      error.value = e
    } finally {
      loading.value = false
      loadingMore.value = false
      loaded.value = true
    }
  }

  // 「加载更多」按钮入口：追加当前游标的下一页（无游标即已到末页，防御性直接返回）。
  function loadMore(): Promise<void> {
    if (!cursor.value) return Promise.resolve()
    return load({ append: true })
  }

  // 整体清空（workspace 切换 / 视角重选）。过滤变化只需 cursor.value 置空后
  // load()：成功会整体覆盖 items，失败时旧游标不残留。
  function reset(): void {
    items.value = []
    cursor.value = null
    loaded.value = false
    error.value = null
  }

  return { items, cursor, loaded, loading, loadingMore, error, load, loadMore, reset }
}
