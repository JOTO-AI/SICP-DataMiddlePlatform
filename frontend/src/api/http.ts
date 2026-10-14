import { session, clearSession } from '../session'
import { responseMetadata, type ResponseHeaders } from './metadata'

export interface Envelope<T> { code: string; message: string; request_id?: string; data: T }
export interface Result<T> { body: T; status: number; requestID: string; snapshot: string; headers: ResponseHeaders }
export type ErrorDomain = 'query' | 'tasks' | 'settings' | 'logs' | 'operations'
export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT'
  body?: unknown
  snapshot?: string
  key?: string
  health?: boolean
  signal?: AbortSignal
  timeoutMs?: number
  domain?: ErrorDomain
}
export class ApiError extends Error {
  constructor(message: string, public code = '', public requestID = '', public status = 0, public headers: ResponseHeaders = Object.freeze({})) {
    super(message)
    this.name = 'ApiError'
  }
}
const common: Record<string, string> = {
  unauthenticated: '登录已过期，请重新登录。',
  forbidden: '当前请求未获授权，请刷新页面后重试。',
  invalid_argument: '请求参数不符合接口约定。',
  request_too_large: '请求内容超过接口允许大小，请减少内容后重试。',
  not_found: '未找到请求的对象。',
  unavailable: '服务暂不可用，请检查服务状态。',
  resource_exhausted: '请求受到资源限制，请稍后重试。',
  deadline_exceeded: '服务处理超时，请稍后重试。',
}
const domainMessages: Record<ErrorDomain, Record<string, string>> = {
  query: {
    not_ready: '服务尚未就绪，请查看运行状态和自动初始化进度。',
    conflict: '查询快照已失效，请重新查询。',
    resource_exhausted: '查询超过资源限制，请缩小查询范围。',
    dependency_failure: '查询依赖暂不可用，请查看服务状态。',
    deadline_exceeded: '查询超时，请缩小范围后重试。',
  },
  tasks: {
    busy: '已有更新或恢复任务正在执行。',
    initialize_required: '自动初始化尚未完成，请查看任务记录。',
    already_initialized: '数据已初始化。',
    not_failed: '只有失败任务可以重跑。',
    idempotency_conflict: '此幂等键已用于其他请求，请核对操作。',
    not_found: '未找到该任务。',
    unavailable: '管理服务暂不可用，受理结果可能未确认。',
  },
  settings: { revision_conflict: '设置版本已变化，请重新读取并比较；本地修改尚未保存。' },
  logs: {
    cursor_expired: '日志游标已过期或保留范围已变化，请重新查询历史日志。',
    proxy_unavailable: '日志代理暂不可用，请检查对应日志源。',
  },
  operations: { revision_conflict: '操作版本、原载荷或前置条件不匹配，请重新读取并核对。' },
}
function domainFor(service: 'query' | 'worker', path: string): ErrorDomain {
  if (service === 'query') return 'query'
  if (path.startsWith('/admin/v1/logs/')) return 'logs'
  if (path.startsWith('/admin/v1/settings/')) return 'settings'
  if (path.startsWith('/admin/v1/tasks/') || path.startsWith('/admin/v1/tables/')) return 'tasks'
  return 'operations'
}

export async function request<T>(service: 'query' | 'worker', path: string, options: RequestOptions = {}): Promise<Result<T>> {
  if (!session.operator) throw new ApiError('请先登录。', 'unauthenticated', '', 401)
  const method = options.method ?? (options.body === undefined ? 'GET' : 'POST')
  if (method === 'GET' && options.body !== undefined) throw new ApiError('GET 请求不能包含请求体。', 'invalid_argument')
  const timeoutMs = options.timeoutMs ?? 30000
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0 || timeoutMs > 2147483647) throw new ApiError('请求超时配置无效。', 'invalid_argument')
  const epoch = session.epoch
  const domain = options.domain ?? domainFor(service, path)
  const mutation = service === 'worker' && method !== 'GET'
  const controller = new AbortController()
  let cause: 'cancelled' | 'timeout' | undefined
  let status = 0, requestID = ''
  let responseHeaders: ResponseHeaders = Object.freeze({})
  const abort = (reason: 'cancelled' | 'timeout') => {
    if (controller.signal.aborted) return
    cause = reason
    controller.abort()
  }
  const externalAbort = () => abort('cancelled')
  options.signal?.addEventListener('abort', externalAbort, { once: true })
  if (options.signal?.aborted) externalAbort()
  const timeout = setTimeout(() => abort('timeout'), timeoutMs)
  function assertCurrent() {
    if (epoch !== session.epoch) throw new ApiError('登录状态已变化，请重新操作。', 'session_changed', requestID, status, responseHeaders)
    if (controller.signal.aborted) {
      const message = cause === 'timeout'
        ? mutation ? '请求超时；操作受理结果未知，请保留原幂等键查询或重试。' : '请求超时，请稍后重试。'
        : mutation ? '已停止等待响应，后台操作不会因此取消。' : '已停止等待响应。'
      throw new ApiError(message, cause, requestID, status, responseHeaders)
    }
  }
  try {
    assertCurrent()
    const headers: Record<string, string> = { Accept: 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf }
    if (options.body !== undefined) headers['Content-Type'] = 'application/json'
    if (options.snapshot) headers['X-Query-Snapshot'] = options.snapshot
    if (options.key) headers['Idempotency-Key'] = options.key
    const endpoint = service === 'worker' && !options.health ? path : `/console-api/${service}${path}`
    const response = await fetch(endpoint, {
      method, headers, body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: controller.signal, cache: 'no-store', credentials: 'same-origin', redirect: 'error',
    })
    responseHeaders = responseMetadata(response.headers)
    status = response.status
    requestID = response.headers.get('X-Request-ID') || ''
    assertCurrent()
    if (status === 401) {
      clearSession('登录已过期，请重新登录。')
      throw new ApiError('登录已过期，请重新登录。', 'unauthenticated', requestID, status, responseHeaders)
    }
    let body: Record<string, unknown>
    try {
      const parsed: unknown = await response.json()
      assertCurrent()
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Expected an object')
      body = parsed as Record<string, unknown>
    } catch (error) {
      assertCurrent()
      if (error instanceof ApiError) throw error
      throw new ApiError(`服务未返回有效 JSON 对象（HTTP ${status}），请检查代理与后端连接。`, 'invalid_response', requestID, status, responseHeaders)
    }
    if (!options.health) {
      if (typeof body.code !== 'string') throw new ApiError('服务响应的 code 不符合当前接口协议。', 'invalid_response', requestID, status, responseHeaders)
      if (!response.ok || !['ok', 'accepted'].includes(body.code)) {
        const message = domainMessages[domain][body.code] ?? common[body.code] ?? (typeof body.message === 'string' && body.message ? body.message : '请求失败。')
        throw new ApiError(message, body.code, typeof body.request_id === 'string' && body.request_id ? body.request_id : requestID, status, responseHeaders)
      }
    }
    return { body: body as T, status, requestID, snapshot: responseHeaders['X-Query-Snapshot'] || '', headers: responseHeaders }
  } catch (error) {
    if (error instanceof ApiError) throw error
    assertCurrent()
    throw new ApiError(mutation ? '连接失败，操作受理结果未知；请保留原幂等键查询或重试。' : '连接失败，请检查后端服务和代理配置。', 'network_error', requestID, status, responseHeaders)
  } finally {
    clearTimeout(timeout)
    options.signal?.removeEventListener('abort', externalAbort)
  }
}
