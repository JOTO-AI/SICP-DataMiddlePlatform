import { test, expect } from '@playwright/test'
import { backendCatalog, backendCapture } from '../fixtures/backend'
import { sourceJobResult } from '../fixtures/sourceTests'
import { ApiError } from '../src/api/http'
import { businessOutcome, sourceOutcome, errorOutcome } from '../src/app/testOutcome'
import type { BusinessTestResult } from '../src/app/businessTest'
import type { SourceTestRead } from '../src/app/sourceTest'
const wrap = (body: unknown): BusinessTestResult => ({ startedAt: '', durationMs: 0, response: { body: body as NonNullable<BusinessTestResult['response']>['body'], status: 200, requestID: '', snapshot: '', headers: {} } })
test('outcomes distinguish all nine recorded successes and empty pages without rewriting backend totals', () => {
  for (const api of backendCatalog() as { id: string; kind: string }[]) {
    if (api.kind !== 'business') continue
    for (const scenario of ['success', 'empty'] as const) {
      const capture = backendCapture(api.id, scenario), before = JSON.stringify(capture.response)
      expect(businessOutcome({ busy: false, result: wrap(capture.response) }).id).toBe(scenario)
      expect(JSON.stringify(capture.response)).toBe(before)
    }
  }
  expect(businessOutcome({ busy: false }).id).toBe('idle')
  expect(businessOutcome({ busy: true }).id).toBe('waiting')
  expect(businessOutcome({ busy: false, validationError: 'missing required field' }).id).toBe('invalid')
  expect(businessOutcome({ busy: false, result: wrap({ code: 'ok', data: null }) }).id).toBe('protocol_failed')
  expect(businessOutcome({ busy: false, result: wrap({ code: 'ok', data: {} }) }).id).toBe('protocol_failed')
})
test('error outcomes preserve backend business errors and distinguish proxy, network, protocol and timeout evidence', () => {
  const captured = backendCapture('students.query', 'snapshot_expired')
  expect(errorOutcome(new ApiError(captured.response.message, captured.response.code, '', captured.status)).id).toBe('business_failed')
  for (const [code, status, expected] of [ ['timeout', 0, 'timeout'], ['deadline_exceeded', 504, 'timeout'], ['invalid_response', 504, 'proxy_failed'], ['invalid_response', 200, 'protocol_failed'], ['network_error', 0, 'connection_failed'], ['unauthenticated', 401, 'access_failed'] ] as const) expect(errorOutcome(new ApiError('', code, '', status)).id).toBe(expected)
})
test('source lifecycle outranks unavailable cache and terminal failures outrank available result bytes', () => {
  for (const [jobStatus, available, scenario, expected] of [ ['queued', false, 'success', 'unconfirmed'], ['running', false, 'success', 'unconfirmed'], ['succeeded', false, 'success', 'unavailable'], ['succeeded', true, 'empty', 'empty'], ['succeeded', true, 'success', 'success'], ['failed', false, 'success', 'source_failed'], ['failed', true, 'success', 'source_failed'], ['interrupted', false, 'success', 'source_failed'] ] as const) {
    const fixture = sourceJobResult('dwd_bzkscjzlb', jobStatus, scenario, available)
    const response = { body: JSON.parse(fixture.raw), resultText: fixture.resultText } as SourceTestRead
    expect(sourceOutcome({ busy: false, pending: true, job: response.body.data.job, response, waitingStopped: false }).id).toBe(expected)
    expect(sourceOutcome({ busy: true, pending: true, job: response.body.data.job, response, waitingStopped: false }).id).toBe('waiting')
  }
  const failed = JSON.parse(sourceJobResult('dwd_bzkscjzlb', 'failed').raw).data.job
  failed.summary.failure = 'request_timeout'
  expect(sourceOutcome({ busy: false, pending: true, job: failed, waitingStopped: false }).id).toBe('timeout')
  failed.summary.failure = 'business'
  expect(sourceOutcome({ busy: false, pending: true, job: failed, waitingStopped: false }).id).toBe('business_failed')
})
