import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { readSystemStatus, statusCount, statusBoolean, statusState } from '../src/app/systemStatus'
test('system status keeps backend partial probes and exact counts without default health', () => {
  const value = managementCapture('system').response.data
  expect(readSystemStatus(value)).toEqual(value)
  for (const unknown of [null, undefined, -1, 1.5, Number.MAX_SAFE_INTEGER + 1]) expect(statusCount(unknown)).toBe('未确认')
  expect(statusCount('9007199254740993')).toBe('9007199254740993')
  expect(statusCount(0)).toBe('0')
  expect(statusBoolean(false)).toBe('尚未就绪'); expect(statusBoolean(null)).toBe('未确认')
  expect(statusState('unknown')).toBe('未知'); expect(statusState('unavailable')).toBe('不可用')
})
import { retentionPayload, readRetention } from '../src/app/retention'
test('retention uses captured payload and enforces both 1..3650 bounds with exact revision', () => {
  const saved = managementCapture('save_retention')
  expect(retentionPayload('1', '30', '30')).toEqual(saved.request)
  expect(readRetention(saved.response.data)).toEqual(saved.response.data)
  expect(retentionPayload('9007199254740993', '1', '3650')).toEqual({ revision: '9007199254740993', platform_days: 1, upstream_days: 3650 })
  for (const days of ['0', '3651', '1.2', '', '-1', '1e2']) expect(() => retentionPayload('1', days, '30')).toThrow()
})