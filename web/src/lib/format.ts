// 跨视图复用的展示格式化。UI 框架选型后如被组件库取代，只动这里。
export function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString()
}
