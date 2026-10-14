import { test, expect } from '@playwright/test'
import { backendCapture, backendCatalog } from '../fixtures/backend'
import type { CatalogAPI } from '../src/app/catalog'
import { businessTestPath, executeBusinessTest } from '../src/app/businessTest'
import { clearSession, session } from '../src/session'
const originalFetch = globalThis.fetch
const catalog = backendCatalog() as CatalogAPI[]
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('nine business tests use exact query proxy contracts and keep actual text and metadata', async () => {
  for (const api of catalog.filter(item => item.kind === 'business')) {
    const captured = backendCapture(api.id), raw = JSON.stringify(captured.response, null, 4) + '\n'
    let requests = 0
    globalThis.fetch = async (url, init) => {
      requests++
      expect(url).toBe('/console-api/query' + api.path)
      expect(init?.method).toBe('POST')
      expect(JSON.parse(init?.body as string)).toEqual(captured.request)
      expect(new Headers(init?.headers).has('X-Query-Snapshot')).toBe(false)
      expect(new Headers(init?.headers).has('X-API-Key')).toBe(false)
      return new Response(raw, { status: captured.status, headers: captured.headers })
    }
    const result = await executeBusinessTest(api, JSON.stringify(captured.request), new AbortController().signal)
    expect(requests).toBe(1)
    expect(result.error).toBeUndefined()
    expect(result.requestText).toBe(JSON.stringify(captured.request))
    expect(result.response?.responseText).toBe(raw)
    expect(result.response?.body).toEqual(captured.response)
    expect(result.response?.requestID).toBe(captured.headers['X-Request-ID'] || captured.response.request_id)
    expect(result.durationMs).toBeGreaterThanOrEqual(0)
  }
})

test('business failures retain backend evidence without retry and untrusted paths cannot execute', async () => {
  const api = catalog.find(item => item.id === 'students.query')!
  const captured = backendCapture(api.id, 'snapshot_expired')
  let requests = 0
  globalThis.fetch = async () => { requests++; return new Response(JSON.stringify(captured.response), { status: captured.status, headers: captured.headers }) }
  const result = await executeBusinessTest(api, JSON.stringify(backendCapture(api.id).request), new AbortController().signal)
  expect(requests).toBe(1)
  expect(result.error?.status).toBe(captured.status)
  expect(result.error?.responseText).toBe(JSON.stringify(captured.response))
  expect(result.error?.code).toBe(captured.response.code)
  expect(() => businessTestPath({ ...api, path: '/admin/v1/api-tests' })).toThrow()
  expect(() => businessTestPath(catalog.find(item => item.kind === 'source')!)).toThrow()
  expect(() => businessTestPath({ ...api, implemented: false })).toThrow()
  expect(requests).toBe(1)
})

test('both timetable executors reject pagination and snapshot tokens before sending', async () => {
  for (const api of catalog.filter(api => api.id.includes('timetable.'))) {
    const capture = backendCapture(api.id)
    let calls = 0
    globalThis.fetch = async (_url, init) => { calls++; expect(JSON.parse(init?.body as string)).toEqual(capture.request); expect(new Headers(init?.headers).has('X-Query-Snapshot')).toBe(false); return new Response(JSON.stringify(capture.response), { status: capture.status, headers: capture.headers }) }
    await executeBusinessTest(api, JSON.stringify(capture.request), new AbortController().signal)
    await expect(executeBusinessTest(api, JSON.stringify(capture.request), new AbortController().signal, '0'.repeat(64))).rejects.toMatchObject({ code: 'snapshot_required' })
    await expect(executeBusinessTest(api, JSON.stringify({ ...capture.request, page: 1 }), new AbortController().signal)).rejects.toThrow('不接受此字段')
    expect(calls).toBe(1)
  }
})
