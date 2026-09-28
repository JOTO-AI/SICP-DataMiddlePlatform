import { reactive } from 'vue'

export const session = reactive({ operator: '', csrf: '', loading: true, notice: '', epoch: 0 })
export function clearSession(message = '') {
  session.operator = ''; session.csrf = ''; session.notice = message; session.epoch++
}
async function authRequest(path: string, body?: unknown) {
  const response = await fetch(`/console-api/${path}`, {
    method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error',
    headers: { 'Content-Type': 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10000),
  })
  const result = await response.json()
  if (!response.ok) {
    if ((path === 'session' || path === 'logout') && response.status === 401) { clearSession(); return }
    throw new Error(response.status === 429 ? '尝试次数过多，请稍后重试。' : response.status === 401 ? '账号或密码不正确。' : '暂时无法完成登录操作，请稍后重试。')
  }
  return result.data as { operator: string; csrf: string } | undefined
}
export async function restoreSession() {
  session.loading = true
  try { const data = await authRequest('session'); if (data) Object.assign(session, data) }
  catch { clearSession('无法连接服务，请检查连接后重试。') }
  finally { session.loading = false }
}
export async function login(username: string, password: string) {
  const data = await authRequest('login', { username, password })
  if (!data?.operator || !data.csrf) throw new Error('登录响应异常，请稍后重试。')
  clearSession(); Object.assign(session, data)
}
export async function logout() {
  await authRequest('logout', {})
  clearSession('已退出登录。')
}
