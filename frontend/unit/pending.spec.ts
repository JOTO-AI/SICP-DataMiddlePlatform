import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { createTaskOperation, createJobOperation } from '../src/api/operations'
import { savePendingRecovery, loadPendingRecovery, clearPendingRecovery, recoveryTTL } from '../src/storage/pending'
const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
const fixture = examples.find((item: { name: string }) => item.name === 'source_test')
const { idempotency_key, ...payload } = fixture.request
const operation = createJobOperation(fixture.path, payload, idempotency_key)
const context = { environment: 'http://sicp-console.test/console/', subject: 'synthetic-admin' }
function memoryStorage() {
  const values = new Map<string, string>()
  return { values, getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) }, removeItem: (key: string) => { values.delete(key) } }
}

test('recovery contains only allowed operation fields and preserves its original expiry', () => {
  const storage = memoryStorage(), now = Date.now()
  expect(savePendingRecovery({ ...context, csrf: 'must-not-persist' } as typeof context, { ...operation, response: fixture.response } as typeof operation, undefined, storage, now)).toBe(true)
  expect(savePendingRecovery(context, operation, `/admin/v1/api-tests/${fixture.response.data.id}`, storage, now + 1000)).toBe(true)
  const [restored] = loadPendingRecovery(context, storage, now + 2000)
  expect(restored?.operation).toEqual(operation)
  expect(restored?.expiresAt).toBe(now + recoveryTTL)
  const raw = [...storage.values.values()][0]!
  expect(raw).not.toContain('must-not-persist')
  expect(raw).not.toContain('response')
  expect(raw).not.toContain('queued')
  expect(loadPendingRecovery(context, storage, now + recoveryTTL)).toEqual([])
  expect(storage.values.size).toBe(0)
})

test('recovery cannot cross environments or subjects and explicit clear removes it', () => {
  for (const other of [{ ...context, environment: 'https://other/console/' }, { ...context, subject: 'other-admin' }]) {
    const storage = memoryStorage()
    savePendingRecovery(context, operation, undefined, storage)
    expect(loadPendingRecovery(other, storage)).toEqual([])
    expect(storage.values.size).toBe(0)
  }
  const storage = memoryStorage()
  savePendingRecovery(context, operation, undefined, storage)
  clearPendingRecovery(storage)
  expect(storage.values.size).toBe(0)
})

test('recovery rejects additional body fields and changed payload for an existing key', () => {
  const storage = memoryStorage()
  for (const field of ['password', 'csrf', 'items', 'result', 'student_number']) {
    const unsafe = createJobOperation(fixture.path, { ...payload, [field]: 'must-not-persist' }, idempotency_key)
    expect(savePendingRecovery(context, unsafe, undefined, storage)).toBe(false)
  }
  expect(storage.values.size).toBe(0)
  expect(savePendingRecovery(context, operation, undefined, storage)).toBe(true)
  const modified = createJobOperation(fixture.path, { ...payload, page_size: payload.page_size + 1 }, idempotency_key)
  expect(savePendingRecovery(context, modified, undefined, storage)).toBe(false)
  expect(loadPendingRecovery(context, storage)[0]?.operation).toEqual(operation)
})

test('corrupt or inaccessible storage cannot break the page and unresolved records are not evicted', () => {
  const storage = memoryStorage()
  storage.setItem('sicp.pending-operations.v1', '{broken')
  expect(loadPendingRecovery(context, storage)).toEqual([])
  const blocked = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') }, removeItem: () => { throw new Error('blocked') } }
  expect(savePendingRecovery(context, operation, undefined, blocked)).toBe(false)
  expect(loadPendingRecovery(context, blocked)).toEqual([])
  for (let index = 0; index < 8; index++) expect(savePendingRecovery(context, createTaskOperation('/admin/v1/tasks/force-full', `protocol-${index}`), undefined, storage)).toBe(true)
  expect(savePendingRecovery(context, operation, undefined, storage)).toBe(false)
  expect(loadPendingRecovery(context, storage)).toHaveLength(8)
})
