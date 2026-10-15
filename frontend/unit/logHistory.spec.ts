import { test, expect } from '@playwright/test'
import { recordedLogPage } from '../fixtures/logTests'
import { backendCapture, managementCapture } from '../fixtures/backend'
import { emptyLogFilters, logHistoryPath, readLogPage } from '../src/app/logHistory'
import { testLogFilters, prepareTestLogs, consumeTestLogs } from '../src/app/testLogNavigation'
import { session } from '../src/session'
test('F06 log queries use real request/job identities and server supported filters', () => {
  const requestID = backendCapture('students.query').headers['X-Request-ID']!
  const business = testLogFilters('business', requestID)!
  const job = managementCapture('source_test').response.data as { id: string }
  const source = testLogFilters('source', requestID, job.id)!
  expect(logHistoryPath(business)).toContain(`request_id=${requestID}`)
  expect(logHistoryPath(source)).toContain(`test_run_id=${job.id}`)
  expect(logHistoryPath(source)).not.toContain('request_id=')
  expect(testLogFilters('source', requestID)).toBeNull()
  expect(testLogFilters('business', 'invalid id')).toBeNull()
  expect(logHistoryPath(source, 'protocol-cursor')).toBe('/admin/v1/logs/upstream/events?cursor=protocol-cursor')
  expect(() => logHistoryPath({ ...business, from: '2026-10-01', to: '2026-10-10' })).toThrow()
  expect(() => logHistoryPath({ ...business, operation: 'unknown' })).toThrow()
  expect(() => logHistoryPath({ ...business, from: '2026-10-01' })).toThrow()
})
test('F06 log page accepts recorded DTOs and rejects wrong range, duplicates and continuation drift', () => {
  const f = emptyLogFilters(), response = recordedLogPage(), p = response.data
  const first = readLogPage(p, f)!
  expect(first).not.toBeNull()
  expect(readLogPage(recordedLogPage('upstream').data, emptyLogFilters('upstream'))).not.toBeNull()
  expect(readLogPage({ ...p, items: [p.items[0], p.items[0]] }, f)).toBeNull()
  expect(readLogPage({ ...p, source: 'upstream' }, f)).toBeNull()
  const epochPage = structuredClone(p); epochPage.epoch = 'protocol_epoch'; epochPage.items[0].epoch = epochPage.epoch
  expect(readLogPage(epochPage, f)).not.toBeNull()
  expect(readLogPage({ ...p, filter: { ...p.filter, kind: 'request' } }, f)).toBeNull()
  expect(readLogPage({ ...p, high_water: '20' }, f)).toBeNull()
  const next = { ...p, items: [] }
  expect(readLogPage(next, f, first)).not.toBeNull()
  expect(readLogPage({ ...next, high_water: '22' }, f, first)).toBeNull()
  expect(readLogPage({ ...next, filter: { ...p.filter, to: '2026-10-15T00:00:00Z' } }, f, first)).toBeNull()
  expect(readLogPage(p, f, first)).toBeNull()
  const byID = { ...f, identity: p.items[0].request_id, format: 'correlation' as const }
  expect(readLogPage({ ...p, filter: { ...p.filter, request_id: byID.identity } }, byID)).not.toBeNull()
  expect(readLogPage({ ...p, filter: { ...p.filter, request_id: 'b'.repeat(64) } }, byID)).toBeNull()
})
test('F06 test log handoff is one use and cleared on session changes', () => {
  session.operator = 'synthetic-admin'
  const f = testLogFilters('business', backendCapture('students.query').headers['X-Request-ID'])!
  expect(prepareTestLogs(f)).toBe(true)
  expect(consumeTestLogs()).toEqual(f); expect(consumeTestLogs()).toBeNull()
  prepareTestLogs(f); session.epoch++
  expect(consumeTestLogs()).toBeNull()
  session.operator = ''; expect(prepareTestLogs(f)).toBe(false)
})

test('F08 phase, time window and explicit raw-versus-correlation formats follow the backend contract', () => {
  const f = emptyLogFilters(), p = recordedLogPage().data
  const phase = { ...f, phase: 'started' }
  expect(new URL(logHistoryPath(phase), 'https://console.invalid').searchParams.get('phase')).toBe('started')
  expect(readLogPage({ ...p, filter: { ...p.filter, phase: 'started' } }, phase)).not.toBeNull()
  expect(readLogPage({ ...p, filter: { ...p.filter, phase: 'finished' } }, { ...phase, phase: 'finished' })).toBeNull()
  expect(readLogPage({ ...p, filter: { ...p.filter, to: '2026-10-13T00:00:00Z' } }, f)).toBeNull()
  expect(readLogPage({ ...p, filter: { ...p.filter, from: '2026-09-01T00:00:00Z' } }, f)).toBeNull()
  expect(() => logHistoryPath({ ...f, phase: 'complete' })).toThrow()
  expect(() => logHistoryPath({ ...f, from: '1999-01-01T00:00:00Z', to: '1999-01-02T00:00:00Z' })).toThrow()
  const id = p.items[0].request_id
  for (const format of ['raw', 'correlation'] as const) {
    const q = new URL(logHistoryPath({ ...f, identity: id, format }), 'https://console.invalid').searchParams
    expect(q.get('request_id')).toBe(id); expect(q.get('id_format')).toBe(format)
    expect(q.has('event_id')).toBe(false); expect(q.has('page')).toBe(false)
  }
})