import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { recordedLogPage } from '../fixtures/logTests'
import { attemptFilters, belongsToCall } from '../src/app/logAttempts'
import { readLogPage, logHistoryPath, type LogEvent } from '../src/app/logHistory'
const recorded = () => managementCapture('log_detail').response.data as unknown as LogEvent
test('recorded request and empty upstream logs cannot become a call chain', () => {
  const request = recorded()
  expect(attemptFilters(request)).toBeNull()
  expect(belongsToCall(request, request)).toBe(false)
  expect(recordedLogPage('upstream').data.items).toEqual([])
})
test('call association protocol requires exact source, epoch and IDs; time proximity is insufficient', () => {
  // Protocol field variations of the captured DTO, not additional normal upstream records.
  const d = recorded(), call = { ...d, source: 'upstream' as const, kind: 'call', call_id: d.request_id }
  expect(attemptFilters({ ...call, call_id: null })).toBeNull()
  expect(attemptFilters({ ...call, request_id: null })).toBeNull()
  const f = attemptFilters(call)!
  expect(f.format).toBe('correlation'); expect(f.identity).toBe(d.request_id)
  const url = new URL(logHistoryPath(f), 'http://localhost')
  expect(url.searchParams.get('kind')).toBe('attempt'); expect(url.searchParams.has('call_id')).toBe(false)
  const item = { ...call, kind: 'attempt', attempt_id: d.event_id, attempt_no: 1 }
  expect(belongsToCall(item, call)).toBe(true)
  for (const change of [{ source: 'platform' }, { epoch: 'other' }, { call_id: 'b'.repeat(64) }, { call_id: null }, { attempt_id: null }, { attempt_no: null }, { kind: 'call' }]) expect(belongsToCall({ ...item, ...change } as LogEvent, call)).toBe(false)
  const page = recordedLogPage('upstream').data
  page.filter = { ...page.filter, ...Object.fromEntries(url.searchParams), page_size: 50 }
  expect(readLogPage(page, f)).not.toBeNull()
})