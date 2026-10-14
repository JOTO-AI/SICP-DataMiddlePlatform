import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { createTaskOperation, createJobOperation, submitOperation, readOperation, type JobPath } from '../src/api/operations'
import { clearSession, session } from '../src/session'
const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
const example = (name: string) => examples.find((item: { name: string }) => item.name === name)
const originalFetch = globalThis.fetch
test.beforeEach(() => { clearSession(); Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf' }) })
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('tasks keep header idempotency and an empty payload across lost replies', async () => {
  const fixture = example('full'), detail = example('task_detail')
  const pending = createTaskOperation(fixture.path, fixture.request_headers['Idempotency-Key'])
  let calls = 0
  globalThis.fetch = async (url, init) => {
    calls++
    expect(url).toBe(fixture.path)
    expect(new Headers(init?.headers).get('Idempotency-Key')).toBe(pending.key)
    expect(JSON.parse(init?.body as string)).toEqual(fixture.request)
    if (calls === 1) throw new TypeError('lost reply')
    return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: { Location: detail.path } })
  }
  await expect(submitOperation(pending)).rejects.toMatchObject({ code: 'network_error' })
  expect(calls).toBe(1)
  const receipt = await submitOperation(pending)
  expect(receipt.location).toBe(detail.path)
  expect(receipt.body).toEqual(fixture.response)
  expect(calls).toBe(2)
})

for (const [name, detailName] of [['source_test', 'test_result'], ['test_notifications', 'notification_test_job'], ['cleanup', 'cleanup_job']]) {
  test(`${name} uses immutable JSON idempotency and reads the original Location`, async () => {
    const fixture = example(name!), detail = example(detailName!)
    const { idempotency_key, ...payload } = structuredClone(fixture.request)
    const pending = createJobOperation(fixture.path as JobPath, payload, idempotency_key)
    // Editing the form after preparation must not alter the accepted operation.
    for (const key of Object.keys(payload)) delete payload[key]
    globalThis.fetch = async (url, init) => {
      expect(new Headers(init?.headers).has('Idempotency-Key')).toBe(false)
      if (init?.method === 'POST') {
        expect(url).toBe(fixture.path)
        expect(JSON.parse(init.body as string)).toEqual(fixture.request)
        return new Response(JSON.stringify(fixture.response), { status: fixture.status, headers: { Location: detail.path } })
      }
      expect(url).toBe(detail.path)
      return new Response(JSON.stringify(detail.response), { status: detail.status })
    }
    const receipt = await submitOperation(pending)
    expect((await readOperation(pending, receipt.location)).body).toEqual(detail.response)
  })
}

test('mismatched and cross-origin receipts never become recovery URLs', async () => {
  const fixture = example('source_test')
  const { idempotency_key, ...payload } = fixture.request
  const pending = createJobOperation(fixture.path, payload, idempotency_key)
  for (const location of ['https://example.invalid/collect', '/admin/v1/api-tests/other-id', '/admin/v1/api-tests/../settings']) {
    globalThis.fetch = async () => new Response(JSON.stringify(fixture.response), { status: 202, headers: { Location: location } })
    await expect(submitOperation(pending)).rejects.toMatchObject({ code: 'invalid_response' })
  }
  expect(() => createJobOperation(fixture.path, payload, 'task-key!')).toThrow('作业幂等键格式无效')
  expect(createTaskOperation(example('full').path, 'task-key!').key).toBe('task-key!')
})
