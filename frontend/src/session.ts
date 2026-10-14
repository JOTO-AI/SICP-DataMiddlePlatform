import { reactive } from 'vue'
import { clearPendingRecovery } from './storage/pending'

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
async function authRequest(path: string, signal: AbortSignal, body?: unknown) {
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal.addEventListener('abort', cancel, { once: true })
  if (signal.aborted) cancel()
  const timer = setTimeout(cancel, 10000)
  try {
    const response = await fetch(`/console-api/${path}`, {
      method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error',
      headers: { 'Content-Type': 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf },
      body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal,
    })
    if ((path === 'session' || path === 'logout') && response.status === 401) return undefined
    const result = await response.json()
    if (controller.signal.aborted) throw new Error('登录状态已变化，请重新操作。')
    if (!response.ok) throw new Error(response.status === 429 ? '尝试次数过多，请稍后重试。' : response.status === 401 ? '账号或密码不正确。' : '暂时无法完成登录操作，请稍后重试。')
    if (result?.code !== 'ok') throw new Error('登录响应异常，请稍后重试。')
    return result.data as { operator: string; csrf: string } | undefined
  } finally { clearTimeout(timer); signal.removeEventListener('abort', cancel) }
}
function applySession(data: { operator: string; csrf: string } | undefined, preserveRecovery = false) {
  if (!data || typeof data.operator !== 'string' || !data.operator || typeof data.csrf !== 'string' || !data.csrf) throw new Error('登录响应异常，请稍后重试。')
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
  } catch { if (run.current()) clearSession('无法连接服务，请检查连接后重试。') }
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
