// Select「全部」哨兵值（TaskFilters/Audit/Messages 等过滤 Select 收口，G4）。
// reka-ui 对 value="" 的 SelectItem 直接 throw（SelectItem 不允许空串），
// 组件挂载中断会在树里留下 component=null 的脏 vnode，路由卸载时崩溃、
// 视图卡死——「全部」选项一律用哨兵值，边界上映射回空串。

export const SELECT_ALL = '__all__'

/** 状态 → Select 值：空串（=全部）换成哨兵，让「全部」项可选中。 */
export function toSelectValue(v: string): string {
  return v || SELECT_ALL
}

/** Select 值 → 状态：哨兵换回空串，下游过滤语义不变。T 支持调用处字面量联合推断。 */
export function fromSelectValue<T extends string = string>(v: unknown): T {
  return (v === SELECT_ALL ? '' : String(v)) as T
}
