import { request, ApiError, type Envelope, type Result } from './http'
import { newIdempotencyKey } from '../idempotency'

export type JobPath = '/admin/v1/api-tests' | '/admin/v1/notifications/test' | '/admin/v1/maintenance/cleanup'
export interface PendingOperation {
  readonly kind: 'task' | 'job'
  readonly path: string
  readonly key: string
  // Serialized once: subsequent edits to the form cannot change a retry.
  readonly payload: string
}
const jobLocations: Record<JobPath, string> = {
  '/admin/v1/api-tests': '/admin/v1/api-tests/',
  '/admin/v1/notifications/test': '/admin/v1/notifications/tests/',
  '/admin/v1/maintenance/cleanup': '/admin/v1/maintenance/cleanup/',
}
const taskPath = /^\/admin\/v1\/(tasks\/(incremental|force-full|[A-Za-z0-9_-]+\/rerun)|tables\/[A-Za-z0-9_-]+\/force-full)$/
function validKey(key: string, kind: 'task' | 'job' = 'task') {
  if (kind === 'job' && !/^[A-Za-z0-9_:-]{1,128}$/.test(key)) throw new ApiError('作业幂等键格式无效。', 'invalid_argument')
  if (!/^[\x21-\x7e]{1,128}$/.test(key) || key.includes(',')) throw new ApiError('幂等键格式无效。', 'invalid_argument')
}
export function createTaskOperation(path: string, key = newIdempotencyKey()): PendingOperation {
  validKey(key)
  if (!taskPath.test(path)) throw new ApiError('任务操作路径无效。', 'invalid_argument')
  return Object.freeze({ kind: 'task', path, key, payload: '{}' })
}
export function createJobOperation(path: JobPath, payload: Record<string, unknown>, key = newIdempotencyKey()): PendingOperation {
  validKey(key, 'job')
  if (!Object.hasOwn(jobLocations, path) || Object.hasOwn(payload, 'idempotency_key')) throw new ApiError('作业路径或幂等键字段无效。', 'invalid_argument')
  const serialized = JSON.stringify({ ...payload, idempotency_key: key })
  return Object.freeze({ kind: 'job', path, key, payload: serialized })
}
function locationPrefix(operation: PendingOperation) {
  if (operation.kind === 'task' && taskPath.test(operation.path)) return '/admin/v1/tasks/'
  if (operation.kind === 'job' && Object.hasOwn(jobLocations, operation.path)) return jobLocations[operation.path as JobPath]
  throw new ApiError('操作路径无效。', 'invalid_argument')
}
export function validateOperationLocation(operation: PendingOperation, location: string): string {
  const prefix = locationPrefix(operation)
  const id = location.startsWith(prefix) ? location.slice(prefix.length) : ''
  if (!/^[A-Za-z0-9_-]+$/.test(id)) throw new ApiError('回执位置不符合当前操作协议，请保留原请求重试。', 'invalid_response')
  return location
}
export async function submitOperation<T extends { task_id?: string; id?: string }>(operation: PendingOperation, signal?: AbortSignal): Promise<Result<Envelope<T>> & { location: string }> {
  locationPrefix(operation)
  validKey(operation.key, operation.kind)
  const body = JSON.parse(operation.payload)
  if (operation.kind === 'task' ? JSON.stringify(body) !== '{}' : body.idempotency_key !== operation.key) throw new ApiError('原请求载荷与幂等键不一致。', 'invalid_argument')
  const result = await request<Envelope<T>>('worker', operation.path, {
    method: 'POST', body, key: operation.kind === 'task' ? operation.key : undefined, signal,
  })
  const id = operation.kind === 'task' ? result.body.data?.task_id : result.body.data?.id
  if (result.status !== 202 || result.body.code !== 'accepted' || typeof id !== 'string' || !/^[A-Za-z0-9_-]+$/.test(id)) throw new ApiError('受理回执不完整，请保留原请求重试。', 'invalid_response', result.requestID, result.status, result.headers)
  const expected = locationPrefix(operation) + id
  // Older gateways may omit Location; the contract's canonical path remains exact.
  const location = validateOperationLocation(operation, result.headers.Location ?? expected)
  if (location !== expected) throw new ApiError('回执位置与作业编号不一致，请保留原请求重试。', 'invalid_response', result.requestID, result.status, result.headers)
  return { ...result, location }
}
export function readOperation<T>(operation: PendingOperation, location: string, signal?: AbortSignal) {
  return request<Envelope<T>>('worker', validateOperationLocation(operation, location), { signal })
}
