import { test, expect } from '@playwright/test'
import { createRequestScope } from '../src/app/requestScope'
import { clearSession, session, login, logout, restoreSession } from '../src/session'
import { request } from '../src/api'
import { backendCapture } from '../fixtures/backend'
const originalFetch = globalThis.fetch
// Authentication protocol identities only; query rows always come from the Go exporter.
const auth = (operator: string) => new Response(JSON.stringify({ code: 'ok', data: { operator, csrf: 'test-csrf' } }))
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'previous-admin', csrf: 'test-csrf' }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('request scopes abort previous generations and reject old session results', () => {
  const scope = createRequestScope()
  const first = scope.begin(), second = scope.begin()
  expect(first.signal.aborted).toBe(true)
  expect(first.current()).toBe(false)
  expect(second.current()).toBe(true)
  clearSession()
  expect(second.current()).toBe(false)
  scope.cancel()
  expect(second.signal.aborted).toBe(true)
})

test('superseded restore cannot replace a successful login even if transport ignores abort', async () => {
  let resolve!: (value: Response) => void, oldSignal: AbortSignal | undefined
  globalThis.fetch = async (url, init) => {
    if (url === '/console-api/session') { oldSignal = init?.signal as AbortSignal; return new Promise<Response>(done => { resolve = done }) }
    return auth('current-admin')
  }
  const restoring = restoreSession()
  await login('current-admin', 'test-password')
  expect(oldSignal?.aborted).toBe(true)
  resolve(auth('previous-admin'))
  await restoring
  expect(session.operator).toBe('current-admin')
  expect(session.loading).toBe(false)
})

test('logout aborts protected reads immediately and late data cannot escape', async () => {
  const fixture = backendCapture('students.query')
  let resolve!: (value: Response) => void, querySignal: AbortSignal | undefined
  globalThis.fetch = async (url, init) => {
    if (url === '/console-api/logout') return new Response(JSON.stringify({ code: 'ok', data: null }))
    querySignal = init?.signal as AbortSignal
    return new Promise<Response>(done => { resolve = done })
  }
  const reading = request('query', '/api/v1/sicp/students/query', { body: fixture.request })
  const rejected = expect(reading).rejects.toMatchObject({ code: 'session_changed' })
  const signingOut = logout()
  expect(querySignal?.aborted).toBe(true)
  await signingOut
  resolve(new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers }))
  await rejected
  expect(session.operator).toBe('')
  expect(session.csrf).toBe('')
})

test('failed logout clears local state without claiming server confirmation', async () => {
  globalThis.fetch = async () => { throw new TypeError('network failure') }
  await expect(logout()).rejects.toThrow('无法连接登录服务')
  expect(session.operator).toBe('')
  expect(session.notice).toContain('退出尚未获得服务端确认')
})
