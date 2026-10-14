import { test, expect } from '@playwright/test'
import { backendCatalog, backendCapture } from '../fixtures/backend'
import { sourceJobResult } from '../fixtures/sourceTests'
import type { CatalogAPI } from '../src/app/catalog'
import { executeBusinessTest } from '../src/app/businessTest'
import { createSourceTest, readSourceTest } from '../src/app/sourceTest'
import { recentTests, clearRecentTests, recordBusinessTest, recordSourceTest, recentTestLimit } from '../src/app/recentTests'
import { clearSession, session } from '../src/session'
import { ApiError } from '../src/api/http'
const catalog = backendCatalog() as CatalogAPI[], originalFetch = globalThis.fetch
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('recent business summaries are allowlisted metadata only, bounded and cleared with the session', async () => {
  const api = catalog.find(api => api.id === 'students.query')!, fixture = backendCapture(api.id)
  globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: fixture.headers })
  const result = await executeBusinessTest(api, JSON.stringify(fixture.request), new AbortController().signal)
  for (let i = 0; i < recentTestLimit + 5; i++) recordBusinessTest(api, result)
  expect(recentTests.value).toHaveLength(50)
  const entry = recentTests.value[0]!
  expect(Object.keys(entry).sort()).toEqual(['apiId', 'durationMs', 'httpStatus', 'jobID', 'jobStatus', 'kind', 'localID', 'observedAt', 'outcome', 'requestID', 'resultState', 'title', 'tone', 'version'].sort())
  expect(entry.requestID).toBe(fixture.headers['X-Request-ID']); expect(entry.httpStatus).toBe(200); expect(entry.jobID).toBeNull()
  const serialized = JSON.stringify(recentTests.value), row = (fixture.response.data as { items: Record<string, unknown>[] }).items[0]!
  expect(serialized).not.toContain(row.student_number as string); expect(serialized).not.toContain(row.name as string)
  expect(serialized).not.toContain('requestText'); expect(serialized).not.toContain('responseText'); expect(serialized).not.toContain(fixture.headers['X-Query-Snapshot']!)
  clearSession(); expect(recentTests.value).toHaveLength(0)
  recordBusinessTest(api, result); expect(recentTests.value).toHaveLength(0)
})

test('source summaries retain only actual job identity and latest availability; errors do not imply cached results remain available', async () => {
  const api = catalog.find(api => api.kind === 'source')!, operation = createSourceTest(api, '{}')
  let fixture = sourceJobResult(api.id)
  globalThis.fetch = async () => new Response(fixture.raw, { status: fixture.status, headers: { 'Content-Type': 'application/json' } })
  let response = await readSourceTest(operation, fixture.path, new AbortController().signal)
  const firstID = recordSourceTest(api, response.body.data.job, response)
  expect(recentTests.value).toHaveLength(1); expect(recentTests.value[0]!.resultState).toBe('available')
  fixture = sourceJobResult(api.id, 'succeeded', 'success', false)
  response = await readSourceTest(operation, fixture.path, new AbortController().signal)
  recordSourceTest(api, response.body.data.job, response)
  expect(recentTests.value).toHaveLength(1); expect(recentTests.value[0]!.localID).toBe(firstID)
  expect(recentTests.value[0]!.outcome).toBe('unavailable'); expect(recentTests.value[0]!.jobStatus).toBe('succeeded')
  expect(JSON.stringify(recentTests.value)).not.toContain('idempotency_key')
  expect(JSON.stringify(recentTests.value)).not.toContain('"summary"'); expect(JSON.stringify(recentTests.value)).not.toContain('"result"')
  const error = new ApiError('untrusted server content must not enter recent summaries', 'network_error')
  recordSourceTest(api, response.body.data.job, response, error, firstID)
  expect(recentTests.value[0]!.resultState).toBe('unconfirmed'); expect(recentTests.value[0]!.httpStatus).toBeNull()
  expect(JSON.stringify(recentTests.value)).not.toContain(error.message)
  clearRecentTests(); expect(recentTests.value).toHaveLength(0)
  recordSourceTest(api, response.body.data.job, response, undefined, firstID)
  expect(recentTests.value).toHaveLength(1); expect(recentTests.value[0]!.localID).not.toBe(firstID)
})