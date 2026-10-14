import { test, expect } from '@playwright/test'
import { backendCatalog, backendCapture, paginationCapture } from '../fixtures/backend'
import type { CatalogAPI } from '../src/app/catalog'
import { executeBusinessTest } from '../src/app/businessTest'
import { firstPageChain, acceptPage, nextPageRequest } from '../src/app/queryPagination'
import { rosterContext, recordRosterPage } from '../src/app/roster'
import { prepareRoster, consumeRoster } from '../src/app/rosterNavigation'
import { responseRecords } from '../src/app/responseViews'
import { clearSession, session } from '../src/session'
const api = (backendCatalog() as CatalogAPI[]).find(api => api.id === 'teaching-classes.students.query')!
const source = backendCapture('staff.teaching-classes.query')
const context = () => rosterContext(responseRecords(JSON.stringify(source.response), 'business').rows[0]!, JSON.stringify(source.request), source.headers['X-SICP-Combination']!, api.version)!
const originalFetch = globalThis.fetch
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('teaching-class handoff uses four exact backend identifiers, no snapshot, once per session', () => {
  const value = context()
  expect(value.request).toEqual(backendCapture(api.id).request)
  expect(value.expectedCount).toBe(3)
  expect(Object.keys(value).sort()).toEqual(['combination', 'expectedCount', 'request', 'version'])
  expect(prepareRoster(value)).toBe(true); expect(consumeRoster()).toEqual(value); expect(consumeRoster()).toBeNull()
  prepareRoster(value); clearSession(); expect(consumeRoster()).toBeNull()
  const row = responseRecords(JSON.stringify(source.response), 'business').rows[0]!
  row.set('planned_course_code', 'null'); expect(rosterContext(row, JSON.stringify(source.request), 'C09', api.version)).toBeNull()
})

test('complete real roster pages deduplicate visited pages and compare only after every page is read', async () => {
  const first = paginationCapture(api.id, 1)
  let calls = 0
  globalThis.fetch = async (_url, init) => {
    const page = JSON.parse(init?.body as string).page, fixture = paginationCapture(api.id, page)
    expect(new Headers(init?.headers).get('X-Query-Snapshot') ?? '').toBe(calls++ ? first.headers['X-Query-Snapshot'] : '')
    return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
  }
  const signal = new AbortController().signal, seed = await executeBusinessTest(api, JSON.stringify(first.request), signal)
  let chain = firstPageChain(api, seed)!, progress = recordRosterPage(context(), chain, seed)
  expect(progress.complete).toBe(false); expect(progress.uniqueCount).toBe(1)
  for (const page of [2, 1, 3]) {
    const result = await executeBusinessTest(api, nextPageRequest(api, chain, page), signal, chain.snapshot)
    chain = acceptPage(chain, result, page); progress = recordRosterPage(context(), chain, result, progress)
  }
  expect(progress.complete).toBe(true); expect(progress.matched).toBe(true)
  expect(progress.pages.size).toBe(3); expect(progress.rowCount).toBe(3); expect(progress.uniqueCount).toBe(3)
})

test('roster validation fails closed on combination, context, duplicate membership and unknown count boundaries', async () => {
  const fixture = backendCapture(api.id), signal = new AbortController().signal
  globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
  const result = await executeBusinessTest(api, JSON.stringify(fixture.request), signal), chain = firstPageChain(api, result)!
  expect(recordRosterPage(context(), chain, result).matched).toBe(true)
  const unknown = recordRosterPage(context(), { ...chain, total: null }, result)
  expect(unknown.total).toBeNull(); expect(unknown.totalPages).toBeNull(); expect(unknown.complete).toBe(false)
  for (const value of [{ ...context(), combination: 'C10' }, { ...context(), version: 'changed' }, { ...context(), request: { ...context().request, semester: '2' } }]) {
    const progress = recordRosterPage(value, chain, result)
    expect(progress.complete).toBe(false); expect(progress.issue).not.toBe('')
  }
  expect(recordRosterPage({ ...context(), expectedCount: null }, chain, result).matched).toBe(false)
  expect(recordRosterPage({ ...context(), expectedCount: 2 }, chain, result).matched).toBe(false)
  expect(recordRosterPage(context(), { ...chain, total: 1001 }, result).issue).toContain('最多')
  // Duplicate an existing backend member as a protocol boundary; no invented person.
  const data = fixture.response.data as { items: Record<string, unknown>[] }
  data.items[1] = structuredClone(data.items[0]!)
  const duplicate = await executeBusinessTest(api, JSON.stringify(fixture.request), signal)
  const progress = recordRosterPage(context(), chain, duplicate)
  expect(progress.complete).toBe(true); expect(progress.uniqueCount).toBe(2); expect(progress.matched).toBe(false); expect(progress.issue).toContain('重复学号')
})

test('each later roster page must keep both its own snapshot and the original class combination', async () => {
  let fixture = paginationCapture(api.id, 1)
  globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
  const signal = new AbortController().signal, result = await executeBusinessTest(api, JSON.stringify(fixture.request), signal), chain = firstPageChain(api, result)!
  const progress = recordRosterPage(context(), chain, result)
  fixture = paginationCapture(api.id, 2)
  const next = await executeBusinessTest(api, nextPageRequest(api, chain, 2), signal, chain.snapshot)
  for (const response of [
    { ...next.response!, snapshot: 'a'.repeat(64) },
    { ...next.response!, headers: { ...next.response!.headers, 'X-SICP-Combination': 'C10' } }
  ]) {
    const failed = recordRosterPage(context(), { ...chain, page: 2 }, { ...next, response }, progress)
    expect(failed.pages.size).toBe(1); expect(failed.matched).toBe(false); expect(failed.complete).toBe(false); expect(failed.issue).not.toBe('')
  }
})