import { ApiError, request, type Envelope, type Result } from '../api/http'
import { createJobOperation, validateOperationLocation, type PendingOperation } from '../api/operations'
import type { CatalogAPI } from './catalog'
import { sourceQueryFields } from './sourceQueryFields'
import { validateTestRequest } from './testRequest'
import { objectTokens } from '../shared/jsonObject'
export interface SourceJob { id: string; action: string; status: string; accepted_at?: string; deadline?: string; started_at?: string | null; finished_at?: string | null; summary: Record<string, unknown> }
export interface SourceTestData { job: SourceJob; result_state: string; result: unknown }
export type SourceTestRead = Result<Envelope<SourceTestData>> & { resultText: string | null }
export function createSourceTest(api: CatalogAPI, text: string, key?: string): PendingOperation {
  if (api.kind !== 'source' || !api.implemented || !Object.hasOwn(sourceQueryFields, api.id) || !/^[A-Za-z0-9_.:-]{1,128}$/.test(api.version)) throw new ApiError('当前目录项不能执行源测试。', 'invalid_catalog')
  const body = validateTestRequest(api, text)
  if (Object.keys(body).some(key => !['page', 'page_size'].includes(key))) throw new ApiError('源测试仅接受分页参数。', 'invalid_argument')
  for (const [name, max] of [['page', 1000000], ['page_size', 100]] as const) {
    const value = body[name]
    if (value != null && (!Number.isSafeInteger(value) || Number(value) < 0 || Number(value) > max)) throw new ApiError(`${name} 不符合源测试分页范围。`, 'invalid_argument')
  }
  return createJobOperation('/admin/v1/api-tests', { api_id: api.id, version: api.version, page: body.page || 1, page_size: body.page_size || 10 }, key)
}
export function terminalSourceJob(job?: SourceJob): boolean { return !!job && ['succeeded', 'failed', 'interrupted', 'partial_failed'].includes(job.status) }
export function validSourceJob(value: unknown, id: string): value is SourceJob {
  if (!value || typeof value !== 'object') return false
  const job = value as SourceJob
  return job.id === id && job.action === 'source_test' && ['queued', 'running', 'succeeded', 'failed', 'interrupted', 'partial_failed'].includes(job.status) && !!job.summary && typeof job.summary === 'object' && !Array.isArray(job.summary)
}
export async function readSourceTest(operation: PendingOperation, location: string, signal: AbortSignal, timeoutMs = 10000): Promise<SourceTestRead> {
  if (operation.kind !== 'job' || operation.path !== '/admin/v1/api-tests') throw new ApiError('源测试作业路径无效。', 'invalid_argument')
  const path = validateOperationLocation(operation, location)
  const response = await request<Envelope<SourceTestData>>('worker', path, { signal, timeoutMs, captureResponse: true })
  const data = response.body.data, id = path.split('/').at(-1)!
  const invalid = () => new ApiError('源测试作业回执不符合协议，请保留作业编号查询。', 'invalid_response', response.requestID, response.status, response.headers, response.responseText)
  if (response.status !== 200 || response.body.code !== 'ok' || !data || !validSourceJob(data.job, id) || !['available', 'result_unavailable'].includes(data.result_state)) throw invalid()
  if (data.result_state === 'available') {
    if (!data.result || typeof data.result !== 'object' || !('list' in data.result) || !Array.isArray(data.result.list) || !('total' in data.result) || typeof data.result.total !== 'number' || !Number.isFinite(data.result.total) || data.result.total < 0) throw invalid()
  } else if (data.result !== null) throw invalid()
  // The HTTP parser checked JSON syntax. Extract raw tokens instead of serializing rounded JS numbers.
  let resultText: string | null
  try { resultText = data.result_state === 'available' ? objectTokens(objectTokens(response.responseText!).get('data')!).get('result') ?? null : null } catch { throw invalid() }
  if (data.result_state === 'available' && resultText === null) throw invalid()
  return { ...response, requestID: response.requestID || response.body.request_id || '', resultText }
}
export function waitForSourcePoll(signal: AbortSignal, ms: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const cancel = () => { clearTimeout(timer); signal.removeEventListener('abort', cancel); reject(new ApiError('已停止等待作业；后台作业不会因此取消。', 'cancelled')) }
    const timer = setTimeout(() => { signal.removeEventListener('abort', cancel); resolve() }, ms)
    signal.addEventListener('abort', cancel, { once: true })
    if (signal.aborted) cancel()
  })
}
