import { test, expect } from '@playwright/test'
import { recordedLogPage } from '../fixtures/logTests'
import { emptyLogFilters, logHistoryPath, readLogPage } from '../src/app/logHistory'
import { initialStreamPath, reconnectDelay, streamFailureAction } from '../src/app/logLive'
test('live initial page keeps server from and separates stream cursor from historical continuation', () => {
  const f = { ...emptyLogFilters(), mode: 'live' as const }, data = recordedLogPage().data
  data.filter.mode = 'live'; data.filter.to = null; data.stream_cursor = 'stream-start'; data.next_cursor = 'history-next'
  const p = readLogPage(data, f)!
  expect(p).not.toBeNull()
  expect(initialStreamPath(p)).toBe('/admin/v1/logs/platform/stream?cursor=stream-start')
  expect(logHistoryPath(f, p.next_cursor!)).toBe('/admin/v1/logs/platform/events?cursor=history-next')
  expect(new URL(logHistoryPath(f), 'http://localhost').searchParams.has('to')).toBe(false)
  expect(readLogPage({ ...data, stream_cursor: null }, f)).toBeNull()
  expect(readLogPage({ ...data, filter: { ...data.filter, to: '2026-10-14T00:00:00Z' } }, f)).toBeNull()
  expect(readLogPage({ ...data, items: [], filter: { ...data.filter, from: '2026-10-12T00:00:01Z' } }, f, p)).toBeNull()
  expect(() => logHistoryPath({ ...f, to: '2026-10-14T00:00:00Z' })).toThrow()
})
test('stream retry policy backs off with server minimum and distinguishes rebuild and permission failure', () => {
  expect([0, 1, 2, 3, 10].map(n => reconnectDelay(n))).toEqual([1000, 2000, 4000, 8000, 30000])
  expect(reconnectDelay(0, 60000)).toBe(60000)
  for (const status of [0, 408, 429, 500, 502, 503, 504]) expect(streamFailureAction(status)).toBe('retry')
  for (const status of [400, 409, 422]) expect(streamFailureAction(status)).toBe('rebuild')
  for (const status of [401, 403, 404, 415]) expect(streamFailureAction(status)).toBe('stop')
})