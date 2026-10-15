import { test, expect } from '@playwright/test'
import { timestampMicros } from '../src/shared/timestamp'
import { recordedLogPage } from '../fixtures/logTests'
import { recordedUpdateEvent } from '../fixtures/updateTests'
import { emptyLogFilters, readLogPage } from '../src/app/logHistory'
import { readUpdatePage } from '../src/app/updateEvents'
test('timestamp boundaries retain real backend microseconds instead of truncating to milliseconds', () => {
  const start = '2026-10-13T00:00:00.000001Z', at = '2026-10-13T00:00:00.000002Z', end = '2026-10-13T00:00:00.000003Z'
  expect(timestampMicros(at)! - timestampMicros(start)!).toBe(1n)
  expect(timestampMicros(at)).toBe(timestampMicros('2026-10-13T08:00:00.000002+08:00'))
  expect(timestampMicros('2026-10-13T00:00:00.0000001Z')).toBeNull()
  const p = recordedLogPage().data; p.filter.from = start; p.filter.to = end; p.items[0].occurred_at = at
  expect(readLogPage(p, emptyLogFilters())).not.toBeNull()
  p.items[0].occurred_at = end; expect(readLogPage(p, emptyLogFilters())).toBeNull()
  const e = { ...recordedUpdateEvent(), accepted_at: at }
  expect(readUpdatePage({ items: [e], next_cursor: null, filter: { from: start, to: end } })).not.toBeNull()
  expect(readUpdatePage({ items: [{ ...e, accepted_at: end }], next_cursor: null, filter: { from: start, to: end } })).toBeNull()
})