// 统一 HTTP 客户端：公共请求头、错误 envelope 解析、request id。
// 所有请求用相对路径（开发期由 Vite 代理，生产同源），禁止硬编码 localhost。

export class AstralApiError extends Error {
  readonly code: string
  readonly retryable: boolean
  readonly details?: Record<string, unknown>
  readonly requestId: string
  readonly status: number

  constructor(status: number, body: { code: string; message: string; retryable: boolean; details?: Record<string, unknown>; request_id: string }) {
    super(body.message)
    this.name = 'AstralApiError'
    this.status = status
    this.code = body.code
    this.retryable = body.retryable
    this.details = body.details
    this.requestId = body.request_id
  }
}

const CLIENT = 'web'
// TODO(phase-1): 与服务端 version 对齐来源（读 package.json 或构建注入），避免双写。
const CLIENT_VERSION = '0.1.0'

function newRequestId(): string {
  // req_ 前缀 + 时间排序随机串；服务端会原样回显。
  return `req_${crypto.randomUUID()}`
}

export interface RequestOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal
  idempotencyKey?: string
}

export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'X-Astral-Client': CLIENT,
    'X-Astral-Client-Version': CLIENT_VERSION,
    'X-Astral-Request-Id': newRequestId(),
  }
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (opts.idempotencyKey) headers['Idempotency-Key'] = opts.idempotencyKey
  // TODO(phase-1): 接入 Web session（HttpOnly Cookie 自动携带，无需手动设置；
  // 若走 Bearer 则由 session store 提供并在此注入）。

  const resp = await fetch(path, {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
    credentials: 'same-origin',
  })

  if (resp.status === 204) return undefined as T

  const text = await resp.text()
  let json: unknown = undefined
  try {
    json = text ? JSON.parse(text) : undefined
  } catch {
    // fallthrough：非 JSON 响应按原始错误处理
  }

  if (!resp.ok) {
    const env = json as { error?: { code: string; message: string; retryable: boolean; details?: Record<string, unknown>; request_id: string } } | undefined
    if (env?.error) {
      throw new AstralApiError(resp.status, {
        ...env.error,
        request_id: env.error.request_id ?? resp.headers.get('X-Astral-Request-Id') ?? '',
      })
    }
    throw new AstralApiError(resp.status, {
      code: 'INTERNAL_ERROR',
      message: `unexpected non-JSON response (${resp.status})`,
      retryable: resp.status >= 500,
      request_id: resp.headers.get('X-Astral-Request-Id') ?? '',
    })
  }
  return json as T
}
