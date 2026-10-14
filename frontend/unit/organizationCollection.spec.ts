import { test, expect } from '@playwright/test'
import { backendCatalog, backendCapture, paginationCapture } from '../fixtures/backend'
import type { CatalogAPI } from '../src/app/catalog'
import { executeBusinessTest } from '../src/app/businessTest'
import { firstPageChain, acceptPage, nextPageRequest } from '../src/app/queryPagination'
import { collectionBlock, collectionRecords, filteredOrganizations, organizationIntegrity, loadOrganizations } from '../src/app/organizationCollection'
import { responseRecords } from '../src/app/responseViews'
import { personFields } from '../src/app/personFields'
import { clearSession, session } from '../src/session'
const originalFetch = globalThis.fetch, api = (backendCatalog() as CatalogAPI[]).find(api => api.id === 'organizations.query')!
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('organization collection explicitly loads every real page on the same snapshot including a nonfirst seed', async () => {
  const first = paginationCapture(api.id, 1), calls: number[] = []
  globalThis.fetch = async (_url, init) => {
    const body = JSON.parse(init?.body as string), page = body.page ?? 1, fixture = paginationCapture(api.id, page)
    expect(body).toEqual(fixture.request)
    expect(new Headers(init?.headers).get('X-Query-Snapshot') ?? '').toBe(calls.length ? first.headers['X-Query-Snapshot'] : '')
    calls.push(page)
    return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
  }
  const signal = new AbortController().signal
  const seed = await executeBusinessTest(api, JSON.stringify(first.request), signal)
  const chain = firstPageChain(api, seed)!
  const second = await executeBusinessTest(api, nextPageRequest(api, chain, 2), signal, chain.snapshot)
  const secondChain = acceptPage(chain, second, 2)
  const progress: number[] = []
  const pages = await loadOrganizations(api, secondChain, second, signal, pages => progress.push(pages.length))
  expect(calls).toEqual([1, 2, 1, 3, 4, 5]); expect(progress).toEqual([1, 2, 3, 4, 5])
  const records = collectionRecords(pages)
  expect(records.objects).toEqual((backendCapture(api.id).response.data as { items: unknown[] }).items)
  expect(organizationIntegrity(records)).toBe(true)
  expect(filteredOrganizations(chain.requestText)).toBe(false)
  expect(filteredOrganizations(JSON.stringify({ ...first.request, is_active: false }))).toBe(true)
  expect(collectionBlock({ ...chain, total: 51 })).toContain('超过')
  expect(collectionBlock({ ...chain, total: 1001, pageSize: 1000 })).toContain('超过')
  const missingParent = { ...records, objects: records.objects.slice(1), rows: records.rows.slice(1) }
  expect(organizationIntegrity(missingParent)).toBe(false)
})

test('organization collection rejects conflicts, changed metadata, duplicates and cancellation without marking completion', async () => {
  for (const boundary of ['conflict', 'combination', 'duplicate', 'cancel'] as const) {
    const first = paginationCapture(api.id, 1), abort = new AbortController()
    let calls = 0, accepted = 0
    globalThis.fetch = async () => {
      calls++
      const fixture = calls === 1 ? first : paginationCapture(api.id, boundary === 'conflict' ? 'snapshot_expired' : boundary === 'duplicate' ? 1 : 2)
      if (calls > 1 && boundary === 'combination') fixture.headers['X-SICP-Combination'] = 'C10'
      if (calls > 1 && boundary === 'cancel') abort.abort()
      return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
    }
    const seed = await executeBusinessTest(api, JSON.stringify(first.request), abort.signal), chain = firstPageChain(api, seed)!
    await expect(loadOrganizations(api, chain, seed, abort.signal, pages => { accepted = pages.length })).rejects.toThrow()
    expect(calls).toBe(2); expect(accepted).toBe(1)
  }
})

test('organization seed validation and bounds perform no extra reads and never turn a partial first page into completeness', async () => {
  const fixture = backendCapture(api.id)
  let calls = 0
  globalThis.fetch = async () => { calls++; return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers }) }
  const signal = new AbortController().signal, seed = await executeBusinessTest(api, JSON.stringify(fixture.request), signal), chain = firstPageChain(api, seed)!
  const pages = await loadOrganizations(api, chain, seed, signal, () => {})
  expect(pages).toHaveLength(1); expect(calls).toBe(1)
  await expect(loadOrganizations(api, { ...chain, total: 1001 }, seed, signal, () => {})).rejects.toThrow('上限')
  const partial = { ...seed, response: { ...seed.response!, body: { ...seed.response!.body, data: { ...(seed.response!.body.data as object), items: [] } } } }
  await expect(loadOrganizations(api, chain, partial, signal, () => {})).rejects.toThrow('未接纳')
  expect(calls).toBe(1)
})

test('person details retain all actual person leaf fields and nulls without guessing code labels', () => {
  for (const id of ['students.query', 'staff.query']) {
    const records = responseRecords(JSON.stringify(backendCapture(id).response), 'business')
    for (const row of records.rows) {
      const details = personFields(row)
      expect(details.truncated).toBe(false)
      for (const [key, raw] of row) {
        if (raw.startsWith('{') && raw !== '{}') expect(details.fields.some(field => field.path.startsWith(key + '.'))).toBe(true)
        else expect(details.fields.find(field => field.path === key)).toBeTruthy()
      }
    }
  }
  // Scalar boundary mutations on a real record; no new normal personnel records.
  const row = responseRecords(JSON.stringify(backendCapture('students.query').response), 'business').rows[0]!
  row.set('status_code', '"UNKNOWN"'); row.set('department', 'null'); row.set('mobile', '"0012345"'); row.set('study_years', '9007199254740993')
  const fields = personFields(row).fields
  expect(fields.find(field => field.path === 'status_code')?.value).toBe('UNKNOWN')
  expect(fields.find(field => field.path === 'department')?.value).toBe('null（未提供）')
  expect(fields.some(field => field.path.startsWith('department.'))).toBe(false)
  expect(fields.find(field => field.path === 'mobile')?.value).toBe('0012345')
  expect(fields.find(field => field.path === 'study_years')?.value).toBe('9007199254740993')
})