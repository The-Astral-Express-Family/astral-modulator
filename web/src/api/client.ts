// 统一 HTTP 客户端（axios 封装）：公共请求头、错误 envelope 解析、request id、
// 失败统一 toast（vue-sonner）。所有请求用相对路径（开发期由 Vite 代理，生产
// 同源），禁止硬编码 localhost。SSE 不走此处（见 sse.ts，EventSource 直连）。

import axios from 'axios'
import type { AxiosError, AxiosInstance, AxiosResponse, Method } from 'axios'
import { toast } from 'vue-sonner'
import type { ApiErrorBody, ErrorCode } from './types'

// 请求级豁免标记：silent 请求失败不弹全局 toast，由调用方按语义消化
// （后台轮询、boot 探测、403 转「无权访问」态、冲突转刷新流等）。
declare module 'axios' {
  export interface AxiosRequestConfig {
    silent?: boolean
  }
}

// 网络层失败（连不上/断网）没有服务端 envelope，用本地码补位。
export type ClientErrorCode = ErrorCode | 'NETWORK_ERROR'

// AstralApiError 是内部错误载体；UI 一律经 formatApiError 消费，勿直接 import。
class AstralApiError extends Error {
  readonly code: ClientErrorCode
  readonly retryable: boolean
  readonly details?: Record<string, unknown>
  readonly requestId: string
  readonly status: number

  constructor(status: number, body: { code: ClientErrorCode; message: string; retryable: boolean; details?: Record<string, unknown>; request_id: string }) {
    super(body.message)
    this.name = 'AstralApiError'
    this.status = status
    this.code = body.code
    this.retryable = body.retryable
    this.details = body.details
    this.requestId = body.request_id
  }
}

/** 面向 UI 的单行错误文案：API 错误带 code，其余原样字符串化。 */
export function formatApiError(e: unknown): string {
  return e instanceof AstralApiError ? `${e.code}: ${e.message}` : String(e)
}

/** 模块函数的可选尾参：标记该次调用为后台请求（失败不弹全局 toast）。 */
export interface CallOpts {
  silent?: boolean
}

const CLIENT = 'web'
// X-Astral-Client-Version 携带协议版本整数（protocol.md §2/§7）：服务端
// 对低于 min_cli_protocol_version 的请求回 400 CLIENT_VERSION_UNSUPPORTED。
// 与服务端 httpx.MinCLIProtocolVersion 同源语义（跨语言双写，两侧同步改）。
const CLIENT_VERSION = '2'

// Bearer 来源由 session store 注入（避免 client→store 循环依赖）。
let tokenProvider: (() => string | null) | null = null

export function setAuthTokenProvider(fn: () => string | null): void {
  tokenProvider = fn
}

// 401 统一出口：由 main.ts 注入（需要 router/session，client 保持零依赖）。
// 返回 true = 已消费（真实会话失效，注入方已提示并跳登录），拦截器不再 toast；
// 返回 false = 匿名期 401（登录失败等），交给拦截器按普通错误提示。
let unauthorizedHandler: (() => boolean) | null = null

export function setUnauthorizedHandler(fn: (() => boolean) | null): void {
  unauthorizedHandler = fn
}

function newRequestId(): string {
  // req_ 前缀 + 随机串；服务端会原样回显。
  return `req_${crypto.randomUUID()}`
}

/** 拼接路径与查询串：undefined/空串字段跳过；查询为空时不追加 ?。 */
export function apiPath(path: string, params: object = {}): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  const qs = q.toString()
  return qs ? `${path}?${qs}` : path
}

export const http: AxiosInstance = axios.create({
  // 相对路径请求：开发期 Vite 代理，生产同源。
  withCredentials: true, // Cookie（HttpOnly refresh token）随同源请求携带
  headers: {
    Accept: 'application/json',
    'X-Astral-Client': CLIENT,
    'X-Astral-Client-Version': CLIENT_VERSION,
  },
})

http.interceptors.request.use((config) => {
  config.headers.set('X-Astral-Request-Id', newRequestId())
  if (config.data !== undefined && config.data !== null) {
    config.headers.set('Content-Type', 'application/json')
  }
  // Bearer access token（内存态）；Cookie 由浏览器自动携带（HttpOnly，JS 不可读）。
  const token = tokenProvider?.()
  if (token) config.headers.set('Authorization', `Bearer ${token}`)
  return config
})

// 204 / 空 body 归一为 data undefined（apiFetch 直接返回 resp.data）。
http.interceptors.response.use((resp) => {
  if (resp.status === 204 || resp.data === '') return { ...resp, data: undefined }
  return resp
})

/** 失败 toast 的统一文案：正文 = 服务端 message，副文 = code + request id（排障线索）。 */
function notifyError(e: AstralApiError): void {
  const description = e.requestId ? `${e.code} · ${e.requestId}` : e.code
  toast.error(e.message || '请求失败', { description, duration: 6000 })
}

/** 非 axios 路径（mock 抛错、本地校验）的手动提示出口：与拦截器同一 toast 样式。 */
export function notifyApiError(e: unknown): void {
  toast.error(e instanceof Error ? e.message : String(e) || '请求失败', { duration: 6000 })
}

/** 响应头读取：AxiosHeaders 大小写不敏感，普通对象按小写键兜底。 */
function headerGet(headers: AxiosResponse['headers'] | undefined, name: string): string {
  if (!headers) return ''
  if (typeof headers.get === 'function') return String(headers.get(name) ?? '')
  return String((headers as Record<string, unknown>)[name.toLowerCase()] ?? '')
}

/** 把 AxiosError 归一为 AstralApiError：优先服务端 envelope，其次非 JSON 兜底，最后网络层。 */
function toApiError(error: AxiosError): AstralApiError {
  const resp = error.response
  if (resp) {
    const requestId = headerGet(resp.headers, 'X-Astral-Request-Id')
    const body = resp.data && typeof resp.data === 'object' ? (resp.data as { error?: ApiErrorBody }).error : undefined
    if (body) {
      return new AstralApiError(resp.status, { ...body, request_id: body.request_id ?? requestId })
    }
    return new AstralApiError(resp.status, {
      code: 'INTERNAL_ERROR',
      message: `unexpected non-JSON response (${resp.status})`,
      retryable: resp.status >= 500,
      request_id: requestId,
    })
  }
  return new AstralApiError(0, {
    code: 'NETWORK_ERROR',
    message: '无法连接服务器，请检查网络或稍后重试。',
    retryable: true,
    request_id: '',
  })
}

http.interceptors.response.use(
  undefined,
  (error: unknown) => {
    // 主动取消（AbortSignal）：不是失败，不提示。
    if (axios.isCancel(error)) throw error
    const axiosError = error as AxiosError
    const apiError = toApiError(axiosError)
    // login/register 的 401 是凭证错误（表单场景），不是会话过期：
    // 不走全局登出出口，按普通错误提示，避免误杀仍然有效的会话。
    const url = axiosError.config?.url ?? ''
    const isCredentialForm = url.startsWith('/api/v1/auth/login') || url.startsWith('/api/v1/auth/register')
    if (apiError.status === 401 && !isCredentialForm) {
      // 真实会话失效由注入方消费（自带提示+跳转）；匿名 401（登录失败等）按普通错误提示。
      const consumed = unauthorizedHandler?.() ?? false
      if (consumed) throw apiError
    }
    if (!axiosError.config?.silent) notifyError(apiError)
    throw apiError
  },
)

export interface RequestOptions extends CallOpts {
  method?: string
  body?: unknown
  signal?: AbortSignal
  idempotencyKey?: string
}

export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const resp = await http.request<unknown, AxiosResponse<unknown>>({
    url: path,
    method: (opts.method ?? 'GET') as Method,
    data: opts.body,
    signal: opts.signal,
    silent: opts.silent,
    headers: opts.idempotencyKey ? { 'Idempotency-Key': opts.idempotencyKey } : undefined,
  })
  return (resp.data ?? undefined) as T
}
