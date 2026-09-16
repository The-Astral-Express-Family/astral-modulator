// documents 作用域 API 模块（S7-5/S7-6）。契约 = api/openapi.yaml documents 段：
// - manifest：GET /workspaces/{id}/documents/manifest —— path 升序，游标 = 末行
//   path（非共享 id 游标），limit 缺省 200 上限 1000，include_deleted=true 含
//   tombstone 行（deleted=true）；
// - get：GET /workspaces/{id}/documents/{path} —— tombstone 也返回（deleted=true，
//   content 照常返回），同步端据此侦测远端删除；
// - delete：DELETE /workspaces/{id}/documents/{path}?base_revision= —— 版本化
//   tombstone（禁止盲删）；base 失配走 409 DOCUMENT_CONFLICT 冲突工件，由
//   ConflictsView 解决；
// - conflicts 列表：GET /workspaces/{id}/conflicts —— status=open|resolved|all，
//   created_at DESC + id 游标（共享 Limit）；
// - conflicts 详情：GET /workspaces/{id}/conflicts/{cid} —— 列表形状 + 双方全文
//   （ours_content / theirs_content；delete 意图工件的 ours 侧无内容与 hash）；
// - resolve：POST /workspaces/{id}/conflicts/{cid}/resolve —— resolution 四选一，
//   merged/manual 必填 content（缺失 400）。
// 类型取自生成物 schema.d.ts（S6-2）；documents 段分页信封是内联对象（非
// Page<T> 泛型），且服务端末页省略 next_cursor 字段（undefined 与 null 同义）。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { components } from '@/api/schema'

export type DocumentDto = components['schemas']['Document']
export type ManifestItemDto = components['schemas']['ManifestItem']
export type DocumentConflictDto = components['schemas']['DocumentConflict']
export type DocumentConflictDetailDto = components['schemas']['DocumentConflictDetail']
export type ConflictResolution = components['schemas']['ConflictResolve']['resolution']

// 分页信封：items 可选（openapi allOf/inline 生成形态），next_cursor 可空。
export interface DocumentEnvelope<T> {
  items?: T[]
  next_cursor?: string | null
}
export type ManifestPage = DocumentEnvelope<ManifestItemDto>
export type ConflictPage = DocumentEnvelope<DocumentConflictDto>

// 文档路径进 URL：'/' 是路由分隔符保持字面量，段内特殊字符逐段编码。
// 服务端 chi 以 /* 尾通配路由并按 RawPath 解码后 canonicalize
// （server document/module.go decodePath），本编码方式与其双向兼容。
function encodeDocPath(path: string): string {
  return path.split('/').map(encodeURIComponent).join('/')
}

export interface ManifestParams {
  limit?: number
  /** path 升序游标 = 上一页末行 path（openapi Cursor 参数） */
  cursor?: string
  include_deleted?: boolean
}

// 受管文档清单（同步基准）。path 升序；next_cursor 非空即还有下一页。
export function getDocumentManifest(
  workspaceId: string,
  params: ManifestParams = {},
  opts: CallOpts = {},
): Promise<ManifestPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/documents/manifest`, params),
    opts,
  )
}

// 读取单个文档（含 tombstone 行：deleted=true + 最后一次内容）。
export function getDocument(
  workspaceId: string,
  path: string,
  opts: CallOpts = {},
): Promise<DocumentDto> {
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/documents/${encodeDocPath(path)}`,
    opts,
  )
}

// 版本化删除（tombstone）：base_revision 必填（缺失 400）；远端已变更时 409
// DOCUMENT_CONFLICT 并落冲突工件。204 无 body。
export function deleteDocument(
  workspaceId: string,
  path: string,
  baseRevision: number,
  opts: CallOpts = {},
): Promise<void> {
  return apiFetch(
    apiPath(
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/documents/${encodeDocPath(path)}`,
      { base_revision: baseRevision },
    ),
    { method: 'DELETE', ...opts },
  )
}

export interface ConflictListParams {
  status?: 'open' | 'resolved' | 'all'
  limit?: number
  cursor?: string
}

// 冲突列表（created_at DESC + id 游标；status 过滤，缺省 open）。
export function listConflicts(
  workspaceId: string,
  params: ConflictListParams = {},
  opts: CallOpts = {},
): Promise<ConflictPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/conflicts`, params),
    opts,
  )
}

// 冲突详情：列表形状 + ours_content / theirs_content 双方全文。
export function getConflict(
  workspaceId: string,
  conflictId: string,
  opts: CallOpts = {},
): Promise<DocumentConflictDetailDto> {
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/conflicts/${encodeURIComponent(conflictId)}`,
    opts,
  )
}

// 解决冲突。resolution=merged/manual 必填 content；已解决再 resolve →
// 409 VALIDATION_FAILED(already_resolved)。成功返回落地后的 Document
// （theirs 分支返回当前行，不产生新 revision）。
export function resolveConflict(
  workspaceId: string,
  conflictId: string,
  payload: components['schemas']['ConflictResolve'],
  opts: CallOpts = {},
): Promise<DocumentDto> {
  return apiFetch(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/conflicts/${encodeURIComponent(conflictId)}/resolve`,
    { method: 'POST', body: payload, ...opts },
  )
}
