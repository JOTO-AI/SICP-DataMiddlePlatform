import { test, expect } from '@playwright/test'
import { diagnosticCapture, diagnosticScenarios } from '../fixtures/diagnostics'
import { requestDiagnostics } from '../src/app/requestDiagnostics'
import { attemptFilters, belongsToCall } from '../src/app/logAttempts'
import { logHistoryPath, readLogEvent } from '../src/app/logHistory'
import { readLogDetail } from '../src/app/logDetail'

test('backend persisted evidence survives task and log DTO presentation without duplicate legacy fields', () => {
  for (const scenario of diagnosticScenarios) {
    const c = diagnosticCapture(scenario), d = c.log_detail
    expect(requestDiagnostics(c.attempt)).toEqual({ items: c.attempt.diagnostics, malformed: false })
    expect(requestDiagnostics(d)).toEqual({ items: c.attempt.diagnostics, malformed: false })
    expect(readLogDetail(d, d.source, d.event_id)).toEqual(d)
    expect(d.event_id).toBe(c.attempt.event_id)
    expect(d.task_id).toBe(c.attempt.task_id)
  }
})
test('legacy authentication fallback keeps each response separate without inventing sent/start facts', () => {
  const { attempt } = diagnosticCapture('page_401_then_token_502')
  const report = requestDiagnostics({ ...attempt, diagnostics: undefined })
  expect(report.items.map(r => [r.kind, r.http_status])).toEqual([['data', 401], ['authentication', 502]])
  expect(report.items[0]?.response?.body).toBe(attempt.response?.body)
  expect(report.items[1]?.response?.body).toBe(attempt.authentication?.response?.body)
  for (const r of report.items) { expect(r.sent).toBeUndefined(); expect(r.started_at).toBeUndefined() }
})
test('malformed optional diagnostics preserve valid peers and never silently fall back to duplicate legacy fields', () => {
  const { attempt } = diagnosticCapture('page_401_then_token_502')
  for (const invalid of [null, {}, 'broken', [null], [{ ...attempt.diagnostics[0], sent: false }], [{ ...attempt.diagnostics[0], response: { read_error: 'false' } }]]) {
    expect(requestDiagnostics({ ...attempt, diagnostics: invalid })).toEqual({ items: [], malformed: true })
  }
  expect(requestDiagnostics({ ...attempt, diagnostics: [attempt.diagnostics[0], null] })).toEqual({ items: [attempt.diagnostics[0]], malformed: true })
  expect(requestDiagnostics({ ...attempt, diagnostics: [] })).toEqual({ items: [], malformed: false })
})
test('protocol boundaries preserve string business codes and distinguish unsent requests', () => {
  const d = diagnosticCapture('unknown_business').attempt.diagnostics[0]!
  // Isolated protocol boundaries, not invented business query results.
  const big = { ...d, business_code: '9223372036854775807' }
  expect(requestDiagnostics({ diagnostics: [big] }).items[0]?.business_code).toBe(big.business_code)
  expect(requestDiagnostics({ diagnostics: [{ ...d, business_code: 98765 }] }).malformed).toBe(true)
  const unsent = { ...d, sent: false, http_status: undefined, response: undefined, stage: 'schedule', failure: 'budget' }
  expect(requestDiagnostics({ diagnostics: [unsent] })).toEqual({ items: [unsent], malformed: false })
})

test('single-log platform evidence can be read and queried without changing its source or hashing IDs again', () => {
  const d = diagnosticCapture('page_401_then_token_502').log_detail
  expect(readLogEvent(d, 'platform')).not.toBeNull()
  // Call/attempt relationship boundary uses the backend-exported correlations.
  const call = { ...d, kind: 'call' }
  const f = attemptFilters(call)!
  expect(f.source).toBe(d.source)
  expect(f.identity).toBe(d.task_id)
  expect(f.format).toBe('correlation')
  expect(logHistoryPath(f)).toContain('/logs/platform/events?')
  expect(logHistoryPath(f)).toContain('kind=attempt')
  expect(belongsToCall(d, call)).toBe(true)
  expect(belongsToCall({ ...d, source: 'upstream' }, call)).toBe(false)
})
