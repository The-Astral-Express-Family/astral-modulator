// 跨视图复用的展示格式化。UI 框架选型后如被组件库取代，只动这里。
export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

/** 实体短 id：列表/徽章里的 12 位前缀 + 省略号（全量见 title 或详情）。 */
export function shortId(id: string): string {
  return id.slice(0, 12) + '…'
}

/** 相对时间（设备列表「最近使用」）：阈值外回退绝对日期。 */
export function fmtRelative(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const minutes = Math.floor(diff / 60_000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} 天前`
  return new Date(iso).toLocaleDateString()
}
