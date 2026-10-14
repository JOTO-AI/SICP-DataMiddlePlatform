import { test, expect } from '@playwright/test'
import { clearSession, login, restoreSession, session } from '../src/session'
const originalFetch = globalThis.fetch
// Authentication protocol cases reflect internal/admin/session.go; no business rows are synthesized.
test.beforeEach(() => clearSession())
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('authentication distinguishes credentials, throttling, proxy errors and malformed success without retries', async () => {
  for (const entry of [
    { status: 401, kind: 'invalid_credentials', message: '账号或密码' },
    { status: 429, kind: 'rate_limited', message: '60 秒' },
    { status: 503, kind: 'unavailable', message: 'HTTP 503' },
    { status: 200, kind: 'invalid_response', message: '格式异常' },
  ]) {
    let requests = 0
    globalThis.fetch = async (_url, init) => { requests++; expect(init?.credentials).toBe('same-origin'); expect(init?.redirect).toBe('error'); return new Response('upstream unavailable', { status: entry.status, headers: { 'Retry-After': '60' } }) }
    await expect(login('protocol-admin', 'protocol-password')).rejects.toMatchObject({ kind: entry.kind, message: expect.stringContaining(entry.message) })
    expect(requests).toBe(1); expect(session.operator).toBe(''); expect(session.csrf).toBe('')
  }
})

test('session restore reports service unavailability and treats an unauthenticated first visit separately', async () => {
  globalThis.fetch = async () => { throw new TypeError('Failed to fetch') }
  await restoreSession()
  expect(session.notice).toContain('无法连接登录服务')
  expect(session.loading).toBe(false)
  globalThis.fetch = async () => new Response('', { status: 401 })
  await restoreSession()
  expect(session.notice).toBe(''); expect(session.operator).toBe('')
})
