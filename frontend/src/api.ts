import { session, clearSession } from './session'
export interface Envelope<T> { code: string; message: string; request_id?: string; data: T }
export interface Result<T> { body: T; status: number; requestID: string; snapshot: string }
export class ApiError extends Error {
  constructor(message: string, public code = '', public requestID = '', public status = 0) { super(message) }
}
const messages: Record<string, string> = {
  unauthenticated: '登录已过期，请重新登录。', forbidden: '当前请求未获授权，请刷新页面后重试。',
  not_ready: '服务尚未就绪，请查看运行状态和自动初始化进度。', conflict: '查询快照已失效，请重新查询。',
  busy: '已有更新或恢复任务正在执行。', initialize_required: '自动初始化尚未完成，请查看任务记录。',
  already_initialized: '数据已初始化。', not_failed: '只有失败任务可以重跑。',
  idempotency_conflict: '此幂等键已用于其他请求，请核对操作。', invalid_argument: '请求参数不符合接口约定。',
  resource_exhausted: '查询超过资源限制，请缩小查询范围。', unavailable: '管理服务暂不可用，受理结果可能未确认。',
  not_found: '未找到该任务。', deadline_exceeded: '查询超时，请缩小范围后重试。',
}
export async function request<T>(service: 'query' | 'worker', path: string, options: { body?: unknown; snapshot?: string; key?: string; health?: boolean } = {}): Promise<Result<T>> {
  if (!session.operator) throw new ApiError('请先登录。')
  const epoch = session.epoch
  const headers: Record<string, string> = { Accept: 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (options.snapshot) headers['X-Query-Snapshot'] = options.snapshot
  if (options.key) headers['Idempotency-Key'] = options.key
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30000)
  try {
    const endpoint = service === 'worker' && !options.health ? path : `/console-api/${service}${path}`
    const response = await fetch(endpoint, { method: options.body === undefined ? 'GET' : 'POST', headers, body: options.body === undefined ? undefined : JSON.stringify(options.body), signal: controller.signal, cache: 'no-store', credentials: 'same-origin', redirect: 'error' })
    if (epoch !== session.epoch) throw new ApiError('登录状态已变化，请重新操作。')
    if (response.status === 401) { clearSession('登录已过期，请重新登录。'); throw new ApiError('登录已过期，请重新登录。') }
    const requestID = response.headers.get('X-Request-ID') || ''
    let body: any
    try {
      body = await response.json()
      if (!body || typeof body !== 'object' || Array.isArray(body)) throw new Error()
    } catch { throw new ApiError('服务未返回有效 JSON 对象，请检查代理与后端连接。', 'invalid_response', requestID, response.status) }
    if (!options.health && (!response.ok || !['ok', 'accepted'].includes(body.code))) throw new ApiError(messages[body.code] || body.message || '请求失败。', body.code, body.request_id || requestID, response.status)
    return { body, status: response.status, requestID, snapshot: response.headers.get('X-Query-Snapshot') || '' }
  } catch (error) {
    if (error instanceof ApiError) throw error
    throw new ApiError(controller.signal.aborted ? '请求超时；管理操作结果未知，请保留原幂等键重试。' : '连接失败，请检查后端服务和代理配置；管理请求可使用原幂等键重试。')
  } finally { clearTimeout(timeout) }
}
export function describe(error: unknown) {
  return error instanceof ApiError ? [error.message, error.code, error.requestID && `请求号 ${error.requestID}`].filter(Boolean).join(' · ') : '操作失败，请重试。'
}
export function time(value?: string | null) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未发生' }
