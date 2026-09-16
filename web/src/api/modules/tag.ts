// tag 作用域 API 模块（S7-2）。契约 = api/openapi.yaml tags 段：
// - 列表：GET /workspaces/{id}/tags（TagPage）；
// - 两步确认（architecture §14）：POST /workspaces/{id}/tag-proposals 发起
//   proposal（action = create|rename|delete；rename/delete 带 target_tag_id），
//   响应回传一次性 confirm_code（TTL 120s，仅明文出现这一次）与全量
//   existing_tags 供比对；POST /tag-proposals/{id}/confirm 提交
//   {confirm_code, name} 落地动作。
// rename/delete 没有独立端点——都经 proposal 流程（本模块唯一事实来源，
//   与 workspace.ts 里的展示用 listTags 并存，不迁移）。
// 类型取自生成物 schema.d.ts（S6-2），路径与契约一字不差。

import { apiFetch, apiPath } from '../client'
import type { CallOpts } from '../client'
import type { components } from '@/api/schema'

export type TagDto = components['schemas']['Tag']
export type TagProposalRequest = components['schemas']['TagProposalRequest']
export type TagProposal = components['schemas']['TagProposal']
export type TagPage = components['schemas']['TagPage']

export interface TagListParams {
  limit?: number
  cursor?: string
}

// workspace tag 列表（字典 + 管理视图数据源）。服务端按成员规模有界一次返回
// （next_cursor 恒空），但信封统一，消费方照常吃 next_cursor。
export function listTags(
  workspaceId: string,
  params: TagListParams = {},
  opts: CallOpts = {},
): Promise<TagPage> {
  return apiFetch(
    apiPath(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tags`, params),
    opts,
  )
}

// 两步确认第一步：不创建正式 tag；确定性预检（create 重名立即 409
// TAG_ALREADY_EXISTS）。Idempotency-Key 由调用方按需传入（重试不重复建 proposal）。
export function proposeTag(
  workspaceId: string,
  payload: TagProposalRequest,
  opts: CallOpts & { idempotencyKey?: string } = {},
): Promise<TagProposal> {
  const { idempotencyKey, ...call } = opts
  return apiFetch(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/tag-proposals`, {
    method: 'POST',
    body: payload,
    ...(idempotencyKey ? { idempotencyKey } : {}),
    ...call,
  })
}

// 两步确认第二步：confirm_code 单次有效（TTL 内）。失败形状（全局 toast 展示）：
// - 403 VALIDATION_FAILED：confirm_code 不匹配（proposal 属他人 / 码错）；
// - 409 TAG_PROPOSAL_EXPIRED：过期或已使用（一次性码）；
// - 409 TAG_ALREADY_EXISTS：confirm 复查撞重名（TOCTOU 兜底）。
export function confirmTagProposal(
  proposalId: string,
  payload: { confirm_code: string; name: string },
  opts: CallOpts = {},
): Promise<TagDto> {
  return apiFetch(`/api/v1/tag-proposals/${encodeURIComponent(proposalId)}/confirm`, {
    method: 'POST',
    body: payload,
    ...opts,
  })
}
