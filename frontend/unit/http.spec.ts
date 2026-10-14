import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { request, ApiError } from '../src/api'
import { session, clearSession } from '../src/session'
import { backendCapture } from '../fixtures/backend'
const originalFetch = globalThis.fetch
const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
const responseFor = (operation: string, scenario: Parameters<typeof backendCapture>[1] = 'success') => {
  const fixture = backendCapture(operation, scenario)
  return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
}
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('HTTP preserves actual backend query response and sends same-origin session headers', async () => {
  const fixture = backendCapture('students.query')
  globalThis.fetch = async (url, init) => {
    expect(url).toBe('/console-api/query/api/v1/sicp/students/query')
    expect(init?.method).toBe('POST')
    expect(init?.credentials).toBe('same-origin')
    expect(init?.redirect).toBe('error')
    expect(new Headers(init?.headers).get('X-SICP-CSRF')).toBe(session.csrf)
    expect(JSON.parse(init?.body as string)).toEqual(fixture.request)
    return responseFor('students.query')
  }
  const result = await request('query', '/api/v1/sicp/students/query', { body: fixture.request })
  expect(result.body).toEqual(fixture.response)
  expect(result.snapshot).toBe(fixture.headers['X-Query-Snapshot'])
})

test('HTTP PUT uses the existing retention example without converting revision', async () => {
  const example = examples.find((item: { pattern: string }) => item.pattern === 'PUT /admin/v1/settings/retention')
  globalThis.fetch = async (url, init) => {
    expect(url).toBe('/admin/v1/settings/retention')
    expect(init?.method).toBe('PUT')
    expect(JSON.parse(init?.body as string)).toEqual(example.request)
    return new Response(JSON.stringify(example.response), { status: example.status })
  }
  expect((await request('worker', '/admin/v1/settings/retention', { method: 'PUT', body: example.request })).body).toEqual(example.response)
})

test('HTTP distinguishes backend snapshot, setting revision and log cursor errors', async () => {
  globalThis.fetch = async () => responseFor('students.query', 'snapshot_expired')
  await expect(request('query', '/api/v1/sicp/students/query', { body: {} })).rejects.toMatchObject({ status: 409, code: 'conflict', message: '查询冲突：可能是快照失效、条件不匹配或教学班跨学期歧义，请核对后重新查询。' })
  // Protocol errors from admin/operations.go and logquery/contract.go, no business rows.
  for (const [path, code, message] of [
    ['/admin/v1/settings/notifications', 'revision_conflict', '设置版本已变化'],
    ['/admin/v1/logs/platform/events', 'cursor_expired', '日志游标已过期'],
    ['/admin/v1/api-tests', 'revision_conflict', '原载荷或前置条件'],
  ]) {
    globalThis.fetch = async () => new Response(JSON.stringify({ code, message: '', data: null }), { status: 409 })
    await expect(request('worker', path)).rejects.toThrow(message)
  }
})

test('HTTP cancellation and timeout are distinct and never retry side effects', async () => {
  let calls = 0
  globalThis.fetch = async (_url, init) => {
    calls++
    return new Promise<Response>((_resolve, reject) => init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true }))
  }
  const controller = new AbortController()
  const pending = request('worker', '/admin/v1/tasks/force-full', { body: {}, key: 'synthetic-task-001', signal: controller.signal })
  controller.abort()
  await expect(pending).rejects.toMatchObject({ code: 'cancelled' })
  await expect(request('worker', '/admin/v1/tasks/force-full', { body: {}, key: 'synthetic-task-001', timeoutMs: 10 })).rejects.toMatchObject({ code: 'timeout' })
  expect(calls).toBe(2)
  await expect(request('worker', '/admin/v1/tasks/', { signal: controller.signal })).rejects.toMatchObject({ code: 'cancelled' })
  expect(calls).toBe(2)
})

test('HTTP handles HTML proxy errors and rejects stale bodies after session replacement', async () => {
  globalThis.fetch = async () => new Response('<html>gateway unavailable</html>', { status: 502, headers: { 'X-Request-ID': 'gateway-request' } })
  await expect(request('worker', '/admin/v1/overview')).rejects.toMatchObject({ code: 'invalid_response', status: 502, requestID: 'gateway-request' })
  let release!: () => void
  globalThis.fetch = async () => {
    const response = responseFor('students.query')
    const json = response.json.bind(response)
    response.json = async () => { await new Promise<void>(resolve => { release = resolve }); return json() }
    return response
  }
  const pending = request('query', '/api/v1/sicp/students/query', { body: {} })
  await expect.poll(() => typeof release).toBe('function')
  clearSession(); Object.assign(session, { operator: 'new-session', csrf: 'new-csrf' })
  release()
  await expect(pending).rejects.toMatchObject({ code: 'session_changed' })
  expect(session.operator).toBe('new-session')
})

test('HTTP retains unauthenticated metadata and distinguishes invalid request options', async () => {
  globalThis.fetch = async () => new Response('', { status: 401, headers: { 'X-Request-ID': 'expired-session' } })
  await expect(request('worker', '/admin/v1/overview')).rejects.toMatchObject({ code: 'unauthenticated', status: 401, requestID: 'expired-session' })
  expect(session.operator).toBe('')
  Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf' })
  await expect(request('worker', '/admin/v1/overview', { method: 'GET', body: {} })).rejects.toBeInstanceOf(ApiError)
  await expect(request('worker', '/admin/v1/overview', { timeoutMs: 0 })).rejects.toMatchObject({ code: 'invalid_argument' })
})

test('HTTP exposes only allowed response headers on success and failure', async () => {
  const fixture = backendCapture('students.query')
  const headers = { ...fixture.headers, Location: '/admin/v1/api-tests/synthetic-job', 'Retry-After': '60', 'Cache-Control': 'no-store', 'Set-Cookie': 'must-not-be-exposed', 'X-Internal-Token': 'must-not-be-exposed' }
  globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: 200, headers })
  const result = await request('query', '/api/v1/sicp/students/query', { body: fixture.request })
  expect(result.headers).toEqual({ ...fixture.headers, Location: headers.Location, 'Retry-After': '60', 'Cache-Control': 'no-store' })
  expect(Object.isFrozen(result.headers)).toBe(true)
  globalThis.fetch = async () => new Response('gateway error', { status: 502, headers })
  await expect(request('worker', '/admin/v1/overview')).rejects.toMatchObject({ status: 502, headers: result.headers })
})

test('HTTP preserves backend decimal string boundaries in both response and PUT payload', async () => {
  // Same boundary as internal/logquery/protocol_test.go; not invented business data.
  const boundary = '9007199254740993'
  const retention = examples.find((item: { pattern: string }) => item.pattern === 'PUT /admin/v1/settings/retention')
  const payload = { ...retention.request, revision: boundary }
  globalThis.fetch = async (_url, init) => {
    expect(JSON.parse(init?.body as string).revision).toBe(boundary)
    return new Response(JSON.stringify({ ...retention.response, data: { ...retention.response.data, revision: '9007199254740994' } }))
  }
  const result = await request<{ data: { revision: string } }>('worker', '/admin/v1/settings/retention', { method: 'PUT', body: payload })
  expect(result.body.data.revision).toBe('9007199254740994')
  const detail = examples.find((item: { pattern: string }) => item.pattern === 'GET /admin/v1/logs/{source}/events/{event_id}')
  expect(typeof detail.response.data.sequence).toBe('string')
  globalThis.fetch = async () => new Response(JSON.stringify({ ...detail.response, data: { ...detail.response.data, sequence: boundary } }))
  const event = await request<{ data: { sequence: string } }>('worker', detail.path)
  expect(event.body.data.sequence).toBe(boundary)
})
