import { reactive } from 'vue'
import { clearPendingRecovery } from './storage/pending'

export class AuthError extends Error {
  constructor(public kind: 'invalid_credentials' | 'rate_limited' | 'unavailable' | 'invalid_response' | 'timeout' | 'cancelled', message: string) { super(message); this.name = 'AuthError' }
}
export const session = reactive({ operator: '', csrf: '', loading: true, notice: '', epoch: 0 })
let protectedController = new AbortController()
let authController: AbortController | undefined
let authGeneration = 0
export function sessionRequestSignal() { return protectedController.signal }
function stopProtectedRequests() {
  const previous = protectedController
  protectedController = new AbortController()
  previous.abort()
}
function stopAuthRequest() { authGeneration++; authController?.abort(); authController = undefined }
export function clearSession(message = '') { resetSession(message, false) }
function resetSession(message: string, preserveRecovery: boolean) {
  if (!preserveRecovery) clearPendingRecovery()
  stopAuthRequest()
  session.operator = ''; session.csrf = ''; session.notice = message; session.epoch++; session.loading = false
  stopProtectedRequests()
}
function startAuth(loading: boolean) {
  stopAuthRequest()
  const generation = authGeneration, epoch = session.epoch
  authController = new AbortController()
  if (loading) session.loading = true
  return { signal: authController.signal, current: () => generation === authGeneration && epoch === session.epoch }
}
async function authRequest(path: 'session' | 'login' | 'logout', signal: AbortSignal, body?: unknown) {
  const controller = new AbortController()
  let timedOut = false
  const cancel = () => controller.abort()
  signal.addEventListener('abort', cancel, { once: true })
  if (signal.aborted) cancel()
  const timer = setTimeout(() => { timedOut = true; cancel() }, 10000)
  function assertCurrent() {
    if (controller.signal.aborted) throw new AuthError(timedOut ? 'timeout' : 'cancelled', timedOut ? '登录服务响应超时，请稍后重试。' : '登录状态已变化，请重新操作。')
  }
  try {
    assertCurrent()
    const response = await fetch(`/console-api/${path}`, {
      method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error',
      headers: { 'Content-Type': 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf },
      body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal,
    })
    assertCurrent()
    if ((path === 'session' || path === 'logout') && response.status === 401) return undefined
    // Error status remains meaningful even when a reverse proxy returns HTML.
    if (response.status === 401) throw new AuthError('invalid_credentials', '账号或密码不正确。')
    if (response.status === 429) {
      const after = response.headers.get('Retry-After') ?? ''
      const seconds = /^\d+$/.test(after) && Number(after) > 0 && Number(after) <= 86400 ? Number(after) : undefined
      throw new AuthError('rate_limited', seconds ? `登录请求受到限制，请至少等待 ${seconds} 秒后再试。` : '登录请求受到限制，请稍后重试。')
    }
    if (!response.ok) throw new AuthError('unavailable', `登录服务暂不可用（HTTP ${response.status}），请稍后重试。`)
    let result: { code?: unknown; data?: { operator: string; csrf: string } }
    try { result = await response.json() } catch { assertCurrent(); throw new AuthError('invalid_response', '登录服务返回格式异常，请检查代理与服务状态。') }
    assertCurrent()
    if (result?.code !== 'ok') throw new AuthError('invalid_response', '登录响应异常，请稍后重试。')
    return result.data
  } catch (cause) {
    assertCurrent()
    if (cause instanceof AuthError) throw cause
    throw new AuthError('unavailable', '无法连接登录服务，请检查网络或服务状态后重试。')
  } finally { clearTimeout(timer); signal.removeEventListener('abort', cancel) }
}
function applySession(data: { operator: string; csrf: string } | undefined, preserveRecovery = false) {
  if (!data || typeof data.operator !== 'string' || !data.operator || typeof data.csrf !== 'string' || !data.csrf) throw new AuthError('invalid_response', '登录响应异常，请稍后重试。')
  resetSession('', preserveRecovery)
  session.operator = data.operator; session.csrf = data.csrf
}
export async function restoreSession() {
  const run = startAuth(true)
  try {
    const data = await authRequest('session', run.signal)
    if (!run.current()) return
    if (data) applySession(data, true)
    else clearSession()
  } catch (error) { if (run.current()) clearSession(error instanceof AuthError ? error.message : '无法连接服务，请检查连接后重试。') }
  finally { if (run.current()) session.loading = false }
}
export async function login(username: string, password: string) {
  const run = startAuth(false)
  const data = await authRequest('login', run.signal, { username, password })
  if (!run.current()) throw new Error('登录状态已变化，请重新操作。')
  applySession(data)
}
export async function logout() {
  clearPendingRecovery()
  const run = startAuth(true)
  stopProtectedRequests()
  try {
    await authRequest('logout', run.signal, {})
    if (run.current()) clearSession('已退出登录。')
  } catch (error) {
    if (run.current()) clearSession('退出尚未获得服务端确认，已清除本地数据；请检查连接后重新登录。')
    throw error
  } finally { if (run.current()) session.loading = false }
}
