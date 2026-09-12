// 路由参数 workspaceId（/workspaces/:workspaceId）的响应式读取；无值时为 ''。

import { computed } from 'vue'
import type { Ref } from 'vue'
import { useRoute } from 'vue-router'

export function useWorkspaceId(): Ref<string> {
  const route = useRoute()
  return computed(() => {
    const raw = route.params.workspaceId
    if (Array.isArray(raw)) return raw[0] ?? ''
    return raw ?? ''
  })
}
