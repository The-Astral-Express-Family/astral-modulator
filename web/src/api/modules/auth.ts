// 认证相关 API。路径与 api/openapi.yaml（D1/D2/D6/A3）一致。
// Web 会话模型：refresh token 存 HttpOnly Cookie，access token 存内存（见 stores/session）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { Actor, ID, PlatformRole } from '../types'

export interface MeResponse {
  actor: Actor
  email?: string
  /** 平台全局角色（round 33）：human ∈ admin/user，agent/service 固化 kind。 */
  platform_role: PlatformRole
  session?: { client_type: 'cli' | 'web'; expires_at?: string }
}

export function login(email: string, password: string): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/login', { method: 'POST', body: { email, password } })
}

export interface RegisterInput {
  email: string
  password: string
  display_name?: string
  registration_code?: string
}

/** 注册（A5/ADR-0009）：registration_code 非空走注册码兑换（仅建号，不入伙）；
 * 为空则是 bootstrap（仅服务器无 human 时开放）。成功即建立 web 会话
 * （服务端已 Set-Cookie），响应与 login 同形状（Me + session）。 */
export function register(input: RegisterInput): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/register', { method: 'POST', body: input })
}

export interface TokenPair {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_token?: string
  actor_id: ID
}

/** 用 HttpOnly Cookie 换新 access token（cookie 模式不传 refresh_token）。
 * 仅 boot 探测 / 静默续期调用：失败不弹 toast（Cookie 兜底或 401 出口接管）。 */
export function refreshWithCookie(): Promise<TokenPair> {
  return apiFetch('/api/v1/auth/token/refresh', { method: 'POST', body: {}, silent: true })
}

export function logout(): Promise<void> {
  return apiFetch('/api/v1/auth/logout', { method: 'POST', body: {} })
}

// ---- 忘记密码（00023，恒 204 防枚举）----

/** 请求重置邮件：无论邮箱是否存在都 204——存在且未停用才真的投递
 * （30 分钟一次性链接；新请求作废旧邮件链接）。 */
export function requestPasswordReset(email: string): Promise<void> {
  return apiFetch('/api/v1/auth/password-reset', { method: 'POST', body: { email } })
}

/** 凭邮件链接里的一次性 token 设置新密码。成功后该账号全部会话被吊销
 * （本浏览器也会被登出，需重新登录）。失败由全局拦截器 toast
 * （PASSWORD_RESET_INVALID / VALIDATION_FAILED）。 */
export function confirmPasswordReset(token: string, newPassword: string): Promise<void> {
  return apiFetch('/api/v1/auth/password-reset/confirm', {
    method: 'POST',
    body: { token, new_password: newPassword },
  })
}

/** boot 会话恢复探测：未登录是常态（401 不提示，匿名期正常路径）。 */
export function getMe(): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/me', { silent: true })
}

export interface UpdateMeInput {
  display_name?: string
  bio?: string
  avatar_url?: string
}

/** 更新自己的资料（PATCH 部分更新：出现的字段才提交）。 */
export function updateMe(input: UpdateMeInput): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/me', { method: 'PATCH', body: input })
}

// ---- 自助改密（协议 2.6）----

/** 修改自己的密码：验证当前密码后设置新密码。成功后除当前浏览器会话外的
 * 全部会话（其他设备/CLI）被注销。失败由全局拦截器 toast
 * （PASSWORD_MISMATCH 当前密码不符 / VALIDATION_FAILED 新密码策略不过）。 */
export function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return apiFetch('/api/v1/auth/password', {
    method: 'POST',
    body: { current_password: currentPassword, new_password: newPassword },
  })
}

// ---- device 审批页（A3）----

export interface DeviceAuthorizationView {
  id: ID
  user_code: string
  client_type: 'cli' | 'web'
  status: 'pending' | 'approved' | 'denied' | 'exchanged' | 'expired'
  created_at: string
  expires_at: string
}

/** 查询设备授权：手动查询走全局 toast；轮询传 silent（网络抖动不打断循环）。 */
export function findDeviceAuthorization(
  userCode: string,
  opts: CallOpts = {},
): Promise<DeviceAuthorizationView> {
  return apiFetch(apiPath('/api/v1/auth/device/authorizations', { user_code: userCode }), opts)
}

export function approveDeviceAuthorization(id: ID): Promise<void> {
  return apiFetch(`/api/v1/auth/device/authorizations/${encodeURIComponent(id)}/approve`, { method: 'POST' })
}

export function denyDeviceAuthorization(id: ID): Promise<void> {
  return apiFetch(`/api/v1/auth/device/authorizations/${encodeURIComponent(id)}/deny`, { method: 'POST' })
}

// ---- 设备管理（会话列表 / 单会话注销）----

/** 一条 session = 一个已登录设备（cli）/浏览器（web）。user_agent 原样返回，友好化在视图层做。 */
export interface SessionView {
  id: ID
  client_type: 'cli' | 'web'
  user_agent: string
  remote_addr: string
  created_at: string
  last_used_at?: string
  expires_at: string
  current: boolean
}

export function listSessions(): Promise<SessionView[]> {
  return apiFetch('/api/v1/auth/sessions')
}

/** 注销自己的一个设备会话；当前浏览器会话走 logout()（清 Cookie）。 */
export function revokeSession(id: ID): Promise<void> {
  return apiFetch(`/api/v1/auth/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
