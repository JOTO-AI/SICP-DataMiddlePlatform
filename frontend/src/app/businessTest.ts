import { request, ApiError, type Envelope, type Result } from '../api'
import type { CatalogAPI } from './catalog'
import { operations } from '../queryOperations'
import { validateTestRequest } from './testRequest'
import { paginatedAPI, validSnapshot } from './queryPagination'
export interface BusinessTestResult { startedAt: string; durationMs: number; requestText?: string; response?: Result<Envelope<unknown>>; error?: ApiError }
export function businessTestPath(api: CatalogAPI): string {
  const operation = operations.find(item => item.operation === api.id)
  if (!operation || api.kind !== 'business' || api.method !== 'POST' || api.path !== operation.path || api.implemented !== true) throw new ApiError('目录接口与已核对的本地查询契约不一致，不能执行。', 'invalid_catalog')
  return operation.path
}
export async function executeBusinessTest(api: CatalogAPI, text: string, signal: AbortSignal, snapshot?: string): Promise<BusinessTestResult> {
  const path = businessTestPath(api), body = validateTestRequest(api, text)
  if (snapshot !== undefined && (!paginatedAPI(api) || !validSnapshot(snapshot))) throw new ApiError('当前接口不接受此查询快照。', 'snapshot_required')
  if (paginatedAPI(api) && !snapshot && Number(body.page ?? 1) !== 1) throw new ApiError('没有本次首响应快照，不能直接续页。请将 page 设为 1 后查询。', 'snapshot_required')
  const requestText = JSON.stringify(body), start = performance.now(), startedAt = new Date().toISOString()
  try {
    const response = await request<Envelope<unknown>>('query', path, { method: 'POST', body, signal, snapshot, captureResponse: true })
    if (response.status !== 200 || response.body.code !== 'ok' || !Object.hasOwn(response.body, 'data')) throw new ApiError('业务响应外壳不符合当前查询协议。', 'invalid_response', response.requestID, response.status, response.headers, response.responseText)
    if (!response.requestID && typeof response.body.request_id === 'string') response.requestID = response.body.request_id
    return { startedAt, requestText, durationMs: Math.max(0, Math.round(performance.now() - start)), response }
  } catch (cause) {
    const error = cause instanceof ApiError ? cause : new ApiError('无法读取查询响应。', 'network_error')
    return { startedAt, requestText, durationMs: Math.max(0, Math.round(performance.now() - start)), error }
  }
}
