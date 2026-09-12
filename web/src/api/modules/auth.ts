// 认证相关 API。路径与 api/openapi.yaml（D1/D2/D6/A3）一致。
// Web 会话模型：refresh token 存 HttpOnly Cookie，access token 存内存（见 stores/session）。

import { apiFetch } from '../client'
import type { Actor, ID } from '../types'

export interface MeResponse {
  actor: Actor
  email?: string
  session?: { client_type: 'cli' | 'web'; expires_at?: string }
}

export function login(email: string, password: string): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/login', { method: 'POST', body: { email, password } })
}

export interface TokenPair {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  refresh_token?: string
  actor_id: ID
}

/** 用 HttpOnly Cookie 换新 access token（cookie 模式不传 refresh_token）。 */
export function refreshWithCookie(): Promise<TokenPair> {
  return apiFetch('/api/v1/auth/token/refresh', { method: 'POST', body: {} })
}

export function logout(): Promise<void> {
  return apiFetch('/api/v1/auth/logout', { method: 'POST', body: {} })
}

export function getMe(): Promise<MeResponse> {
  return apiFetch('/api/v1/auth/me')
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

// ---- device 审批页（A3）----

export interface DeviceAuthorizationView {
  id: ID
  user_code: string
  client_type: 'cli' | 'web'
  status: 'pending' | 'approved' | 'denied' | 'exchanged' | 'expired'
  created_at: string
  expires_at: string
}

export function findDeviceAuthorization(userCode: string): Promise<DeviceAuthorizationView> {
  return apiFetch(`/api/v1/auth/device/authorizations?user_code=${encodeURIComponent(userCode)}`)
}

export function approveDeviceAuthorization(id: ID): Promise<void> {
  return apiFetch(`/api/v1/auth/device/authorizations/${encodeURIComponent(id)}/approve`, { method: 'POST' })
}

export function denyDeviceAuthorization(id: ID): Promise<void> {
  return apiFetch(`/api/v1/auth/device/authorizations/${encodeURIComponent(id)}/deny`, { method: 'POST' })
}
