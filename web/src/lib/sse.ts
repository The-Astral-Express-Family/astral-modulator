// SSE 展示层共享常量（三视图重复定义收敛，G4）。SseState 的事实声明在
// api/sse.ts（传输层，刻意零 UI 依赖）；此处只做状态 → 徽章变体的展示映射。

import type { SseState } from '@/api/sse'
import type { BadgeVariants } from '@/components/ui/badge'

export const SSE_VARIANTS: Record<SseState, BadgeVariants['variant']> = {
  connecting: 'secondary',
  open: 'default',
  closed: 'outline',
}
