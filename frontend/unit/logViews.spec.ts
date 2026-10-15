import { test, expect } from '@playwright/test'
import { logViewFilters, selectedLogView } from '../src/app/logViews'
import { logHistoryPath, readLogPage } from '../src/app/logHistory'
import { recordedLogPage } from '../fixtures/logTests'
test('four log views bind existing backend source and kind without filtering only the visible page', () => {
  for (const [view, source, kind] of [['requests', 'platform', 'request'], ['updates', 'platform', 'task'], ['upstream', 'upstream', ''], ['audit', 'platform', 'audit']]) {
    const f = logViewFilters(view!)!, query = new URL(logHistoryPath(f), 'https://console.invalid')
    expect(query.pathname).toBe(`/admin/v1/logs/${source}/events`)
    expect(query.searchParams.get('kind')).toBe(kind || null); expect(selectedLogView(f)).toBe(view)
  }
  const f = logViewFilters('requests')!, page = recordedLogPage().data
  page.filter.kind = 'request'; expect(readLogPage(page, f)).not.toBeNull()
  expect(readLogPage(page, logViewFilters('audit')!)).toBeNull()
  expect(() => logHistoryPath({ ...f, source: 'upstream' })).toThrow()
  expect(logViewFilters('unknown')).toBeNull()
})