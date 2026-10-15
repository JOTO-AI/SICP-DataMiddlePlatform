import { test, expect } from '@playwright/test'
import { recordedLogPage } from '../fixtures/logTests'
import { emptyLogFilters, logHistoryPath, readLogPage } from '../src/app/logHistory'
import { initialStreamPath } from '../src/app/logLive'
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