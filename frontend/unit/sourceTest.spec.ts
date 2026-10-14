import { test, expect } from '@playwright/test'
import { backendCatalog, managementCapture } from '../fixtures/backend'
import { sourceJobResult } from '../fixtures/sourceTests'
import { createSourceTest, readSourceTest, terminalSourceJob, waitForSourcePoll } from '../src/app/sourceTest'
import { submitOperation } from '../src/api/operations'
import type { CatalogAPI } from '../src/app/catalog'
import { clearSession, session } from '../src/session'
const originalFetch = globalThis.fetch
const catalog = backendCatalog() as CatalogAPI[], source = catalog.find(api => api.id === 'dwd_bzkscjzlb')!
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf', loading: false }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('source job submission fixes context and normalized paging with one immutable idempotency key', async () => {
  const accepted = managementCapture('source_test'), lookup = managementCapture('test_result')
  for (const text of ['{}', '{"page":null,"page_size":0}', '{"page":1,"page_size":10}']) {
    const operation = createSourceTest(source, text, 'synthetic-test-001')
    expect(JSON.parse(operation.payload)).toEqual({ api_id: source.id, version: source.version, page: 1, page_size: 10, idempotency_key: 'synthetic-test-001' })
    let count = 0
    globalThis.fetch = async (url, init) => {
      count++; expect(url).toBe(accepted.path); expect(init?.method).toBe('POST')
      expect(init?.body).toBe(operation.payload)
      expect(new Headers(init?.headers).has('Idempotency-Key')).toBe(false)
      return new Response(JSON.stringify(accepted.response), { status: accepted.status, headers: { Location: lookup.path } })
    }
    expect((await submitOperation(operation)).location).toBe(lookup.path)
    expect(count).toBe(1)
  }
  expect(() => createSourceTest(catalog.find(api => api.kind === 'business')!, '{}')).toThrow()
  expect(() => createSourceTest(source, '{"page_size":101}')).toThrow()
  expect(() => createSourceTest(source, '{"api_id":"other"}')).toThrow()
})

test('source reads preserve all fourteen generated mock result tokens and never follow external locations', async () => {
  for (const api of catalog.filter(api => api.kind === 'source')) {
    const fixture = sourceJobResult(api.id), operation = createSourceTest(api, '{}')
    let calls = 0
    globalThis.fetch = async (url, init) => { calls++; expect(url).toBe(fixture.path); expect(init?.method).toBe('GET'); expect(init?.body).toBeUndefined(); return new Response(fixture.raw, { status: fixture.status }) }
    const read = await readSourceTest(operation, fixture.path, new AbortController().signal)
    expect(read.responseText).toBe(fixture.raw)
    expect(read.resultText).toBe(fixture.resultText)
    expect(terminalSourceJob(read.body.data.job)).toBe(true)
    await expect(readSourceTest(operation, 'https://untrusted.example' + fixture.path, new AbortController().signal)).rejects.toThrow()
    expect(calls).toBe(1)
  }
})

test('source queued unavailable is pending; mismatched identities and inconsistent result envelopes fail closed', async () => {
  const fixture = managementCapture('test_result'), operation = createSourceTest(source, '{}')
  globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: fixture.status })
  const read = await readSourceTest(operation, fixture.path, new AbortController().signal)
  expect(terminalSourceJob(read.body.data.job)).toBe(false)
  expect(read.resultText).toBeNull()
  for (const mutation of [ { job: { ...read.body.data.job, id: 'other' } }, { result_state: 'available' }, { result: {} }, { job: { ...read.body.data.job, status: 'future-status' } } ]) {
    globalThis.fetch = async () => new Response(JSON.stringify({ ...fixture.response, data: { ...read.body.data, ...mutation } }), { status: fixture.status })
    await expect(readSourceTest(operation, fixture.path, new AbortController().signal)).rejects.toThrow('回执不符合协议')
  }
  const controller = new AbortController(), wait = waitForSourcePoll(controller.signal, 30000)
  controller.abort()
  await expect(wait).rejects.toThrow('停止等待')
})
