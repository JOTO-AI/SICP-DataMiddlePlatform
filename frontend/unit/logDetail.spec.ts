import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { readLogDetail } from '../src/app/logDetail'
import { readLogEvent } from '../src/app/logHistory'
test('log detail preserves the existing backend DTO, distinct times and missing relationships', () => {
  const raw = managementCapture('log_detail').response.data as any
  const d = readLogDetail(raw, raw.source, raw.event_id, raw.epoch, raw.sequence)!
  expect(d).toEqual(raw); expect(d.phase).toBe('started'); expect(d.outcome).toBeNull()
  expect(d.call_id).toBeNull(); expect(d.attempt_id).toBeNull(); expect(d.business_code).toBeNull()
  expect(d.redaction.original_bytes).toBe('128')
  expect(readLogEvent(raw, raw.source, raw.epoch)).not.toBeNull()
})
test('log detail fails closed on source, epoch, event identity and malformed redaction fields', () => {
  const d = managementCapture('log_detail').response.data as any
  expect(readLogDetail(d, 'upstream', d.event_id)).toBeNull()
  expect(readLogDetail(d, 'platform', d.event_id, 'different_epoch')).toBeNull()
  expect(readLogDetail(d, 'platform', d.event_id, d.epoch, '22')).toBeNull()
  expect(readLogDetail(d, 'platform', 'b'.repeat(64))).toBeNull()
  for (const change of [{ redaction: null }, { redaction: { ...d.redaction, truncated: 'false' } }, { response: { body: false } }, { call_id: 'raw-call-id' }, { attempt_no: 0 }, { received_at: 'not-a-time' }]) expect(readLogDetail({ ...d, ...change }, 'platform', d.event_id)).toBeNull()
})