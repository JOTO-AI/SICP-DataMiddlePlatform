import { test, expect } from '@playwright/test'
import { backendCatalog, paginationCapture, backendCapture } from '../fixtures/backend'
import type { CatalogAPI } from '../src/app/catalog'
import { firstPageChain, nextPageRequest, acceptPage, paginatedAPI, pageCount } from '../src/app/queryPagination'
import { executeBusinessTest } from '../src/app/businessTest'
import { clearSession, session } from '../src/session'
import { ApiError } from '../src/api/http'
const originalFetch = globalThis.fetch, catalog = backendCatalog() as CatalogAPI[]
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('seven pagination chains replay every actual backend page with unchanged conditions and one interface snapshot', async () => {
  expect(catalog.filter(paginatedAPI)).toHaveLength(7)
  for (const api of catalog.filter(paginatedAPI)) {
    const first = paginationCapture(api.id, 1)
    let fixture = first
    globalThis.fetch = async (url, init) => {
      expect(url).toBe('/console-api/query' + api.path)
      expect(JSON.parse(init?.body as string)).toEqual(fixture.request)
      expect(new Headers(init?.headers).get('X-Query-Snapshot') ?? '').toBe(fixture.request_snapshot ?? '')
      return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
    }
    const result = await executeBusinessTest(api, JSON.stringify(first.request), new AbortController().signal)
    let chain = firstPageChain(api, result)!
    expect(chain.blocked).toBe('')
    const rows = [...(result.response!.body.data as { items: unknown[] }).items]
    for (let page = 2; page <= pageCount(chain); page++) {
      fixture = paginationCapture(api.id, page)
      const next = await executeBusinessTest(api, nextPageRequest(api, chain, page), new AbortController().signal, chain.snapshot)
      chain = acceptPage(chain, next, page)
      rows.push(...(next.response!.body.data as { items: unknown[] }).items)
      expect(chain.page).toBe(page)
    }
    expect(rows).toEqual((backendCapture(api.id).response.data as { items: unknown[] }).items)
    expect(() => nextPageRequest(catalog.find(other => paginatedAPI(other) && other.id !== api.id)!, chain, 1)).toThrow('当前接口')
  }
})

test('missing snapshots, changed combination and actual backend conflicts cannot silently start a new chain', async () => {
  const api = catalog.find(api => api.id === 'students.query')!, first = paginationCapture(api.id, 1)
  let fixture = first, calls = 0
  globalThis.fetch = async () => { calls++; return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers }) }
  const result = await executeBusinessTest(api, JSON.stringify(first.request), new AbortController().signal)
  const chain = firstPageChain(api, result)!
  const missing = firstPageChain(api, { ...result, response: { ...result.response!, snapshot: '' } })!
  expect(missing.blocked).toContain('不能续页')
  expect(() => nextPageRequest(api, missing, 2)).toThrow()
  fixture = paginationCapture(api.id, 2)
  await expect(executeBusinessTest(api, JSON.stringify(fixture.request), new AbortController().signal)).rejects.toMatchObject({ code: 'snapshot_required' })
  expect(calls).toBe(1)
  const next = await executeBusinessTest(api, nextPageRequest(api, chain, 2), new AbortController().signal, chain.snapshot)
  const changed = { ...next, response: { ...next.response!, headers: { ...next.response!.headers, 'X-SICP-Combination': 'C10' } } }
  expect(() => acceptPage(chain, changed, 2)).toThrow('本页未接纳')
  for (const scenario of ['snapshot_conflict', 'snapshot_expired'] as const) {
    fixture = paginationCapture(api.id, scenario)
    const failed = await executeBusinessTest(api, nextPageRequest(api, chain, 2), new AbortController().signal, chain.snapshot)
    expect(failed.error?.code).toBe('conflict')
    expect(acceptPage(chain, failed, 2).blocked).toContain('不会自动重试')
  }
  expect(acceptPage(chain, { startedAt: '', durationMs: 0, error: new ApiError('', 'other_code', '', 409) }, 2).blocked).toBe('')
  const unknown = firstPageChain(api, { ...result, response: { ...result.response!, body: { ...result.response!.body, data: { items: [], total: null } } } })!
  expect(unknown.total).toBeNull(); expect(unknown.blocked).toContain('总数')
  const malformed = firstPageChain(api, { ...result, response: { ...result.response!, body: { ...result.response!.body, data: { total: 12, items: null } } } })!
  expect(malformed.blocked).toContain('首响应记录结构')
})
