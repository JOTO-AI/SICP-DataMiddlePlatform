import { test, expect } from '@playwright/test'
import { createReadLimiter, ReadQueueFullError } from '../src/api/readLimiter'
import { pollingDelay } from '../src/app/polling'
import { request } from '../src/api'
import { clearSession, session } from '../src/session'
const originalFetch = globalThis.fetch
test.afterEach(() => { globalThis.fetch = originalFetch; clearSession() })

test('read limiter bounds active and queued reads, cancels queued entries and releases once', async () => {
  const limiter = createReadLimiter(1, 1), controller = new AbortController()
  const first = await limiter.acquire(new AbortController().signal)
  const queued = limiter.acquire(controller.signal)
  const cancelled = expect(Promise.resolve(queued)).rejects.toThrow('Aborted')
  expect(() => limiter.acquire(new AbortController().signal)).toThrow(ReadQueueFullError)
  controller.abort()
  await cancelled
  const following = limiter.acquire(new AbortController().signal)
  first(); first()
  const release = await following
  let admitted = false
  const final = Promise.resolve(limiter.acquire(new AbortController().signal)).then(done => { admitted = true; return done })
  await Promise.resolve()
  expect(admitted).toBe(false)
  release()
  ;(await final)()
})

test('HTTP limits simultaneous GETs to four and cancels a queued read without fetching', async () => {
  Object.assign(session, { operator: 'synthetic-admin', csrf: 'synthetic-csrf' })
  const releases: Array<() => void> = []
  let calls = 0
  globalThis.fetch = async () => {
    calls++
    await new Promise<void>(resolve => { releases.push(resolve) })
    return new Response(JSON.stringify({ live: true }))
  }
  const reads = Array.from({ length: 4 }, () => request('worker', '/livez', { health: true }))
  const controller = new AbortController()
  const waiting = request('worker', '/readyz', { health: true, signal: controller.signal })
  const cancelled = expect(waiting).rejects.toMatchObject({ code: 'cancelled' })
  expect(calls).toBe(4)
  controller.abort()
  await cancelled
  for (const release of releases) release()
  await Promise.all(reads)
  expect(calls).toBe(4)
})

test('read polling uses bounded exponential delays and valid Retry-After values', () => {
  expect([0, 1, 2, 3, 100].map(failures => pollingDelay(10000, failures))).toEqual([10000, 20000, 40000, 60000, 60000])
  expect(pollingDelay(10000, 1, '90')).toBe(90000)
  expect(pollingDelay(10000, 1, '999999')).toBe(300000)
  expect(pollingDelay(10000, 1, 'invalid')).toBe(20000)
})
