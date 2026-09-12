// 任务视图共用的枚举展示元数据（状态/优先级标签与颜色）与错误码提取。
import type { TaskPriority, TaskStatus } from '@/api/types'

export const TASK_STATUS_META: Record<TaskStatus, { label: string; dot: string }> = {
  open: { label: '待办', dot: 'bg-muted-foreground/40' },
  in_progress: { label: '进行中', dot: 'bg-blue-500' },
  blocked: { label: '受阻', dot: 'bg-red-500' },
  review: { label: '评审中', dot: 'bg-amber-500' },
  done: { label: '完成', dot: 'bg-emerald-500' },
  cancelled: { label: '取消', dot: 'bg-zinc-400' },
}

export const TASK_STATUSES = Object.keys(TASK_STATUS_META) as TaskStatus[]

export const TASK_PRIORITY_META: Record<
  TaskPriority,
  { label: string; icon: string | null }
> = {
  urgent: { label: '紧急', icon: 'text-red-500' },
  high: { label: '高', icon: 'text-orange-500' },
  normal: { label: '普通', icon: null },
  low: { label: '低', icon: 'text-muted-foreground/40' },
}

export const TASK_PRIORITIES = Object.keys(TASK_PRIORITY_META) as TaskPriority[]

// 从错误对象提取稳定码：真实 API 错误带 .code；mock 错误是 `CODE: 文案` 文本。
export function apiErrorCode(e: unknown): string | null {
  if (e && typeof e === 'object' && 'code' in e) return String((e as { code: unknown }).code)
  const m = e instanceof Error ? /^([A-Z_]+):/.exec(e.message) : null
  return m?.[1] ?? null
}
