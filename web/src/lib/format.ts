// 跨视图复用的展示格式化。UI 框架选型后如被组件库取代，只动这里。
export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

/** 实体短 id：列表/徽章里的 12 位前缀 + 省略号（全量见 title 或详情）。 */
export function shortId(id: string): string {
  return id.slice(0, 12) + '…'
}
