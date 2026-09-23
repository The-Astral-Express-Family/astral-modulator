// 「上次停留位置」的持久化（localStorage）：应用内导航始终以 URL 为唯一事实源
// （工作区页刷新留原页由 /workspaces/:id 路径本身保证），本模块只服务两类
// 无工作区上下文的落点——冷启动落在 /（总览）、登录/注册后无 from 回跳——
// 让用户回到上次工作的工作区页面。登出即清除（防共用机器把下个登录者
// 送进前一个用户的上下文）。

import { sanitizeRedirect } from './redirect'

const STORAGE_KEY = 'astral:last-location'

/** 读取上次停留位置；经 sanitizeRedirect 净化（防存储被污染成外链/登录循环）。 */
export function readLastLocation(): string | null {
  try {
    return sanitizeRedirect(localStorage.getItem(STORAGE_KEY))
  } catch {
    return null
  }
}

export function recordLocation(fullPath: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, fullPath)
  } catch {
    // 隐私模式等写入失败可接受：只损失「回到上次位置」这一便利。
  }
}

export function clearLastLocation(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // 同上。
  }
}
