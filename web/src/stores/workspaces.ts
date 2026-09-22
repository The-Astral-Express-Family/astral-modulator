// workspace 列表单源 store：切换器与创建对话框共用同一份列表，
// 创建成功后 refresh() 即时可见，无需组件间事件。加载失败静默——
// 侧栏导航不阻塞，错误由页面自身呈现。

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listWorkspaces } from '../api/modules/core'
import type { Workspace } from '../api/types'

export const useWorkspacesStore = defineStore('workspaces', () => {
  const items = ref<Workspace[]>([])
  const loaded = ref(false)
  let inflight: Promise<void> | null = null

  /** ensure 只拉一次；refresh(force) 创建等场景强制重拉。并发调用共享同一 Promise。 */
  function load(force = false): Promise<void> {
    if (loaded.value && !force) return Promise.resolve()
    if (!inflight) {
      inflight = listWorkspaces({ limit: 200 })
        .then((page) => {
          items.value = page.items
          loaded.value = true
        })
        .catch(() => {
          items.value = []
          loaded.value = true
        })
        .finally(() => {
          inflight = null
        })
    }
    return inflight
  }

  return { items, loaded, load }
})
