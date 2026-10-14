import { test, expect } from '@playwright/test'
import { progressCounter, progressPercent } from '../src/workProgress'

test('progress counters follow backend omitempty and reject invalid numeric boundaries', () => {
  expect(progressCounter(undefined)).toBe(0) // internal/activity/activity.go Snapshot JSON contract
  for (const value of [null, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) expect(progressCounter(value)).toBeUndefined()
  for (const total of [null, undefined, 0, -1, NaN]) expect(progressPercent(0, total)).toBeUndefined()
  expect(progressPercent(1, 2, false)).toBeUndefined()
  expect(progressPercent(3, 2)).toBeUndefined()
})

test('work percentage uses current attempt, not cumulative retry work', () => {
  // Values from TestBT09_RealWorkRetryAndHeartbeatAreSeparate in internal/activity/activity_test.go.
  expect(progressPercent(3, 10)).toBe(30)
  expect(progressPercent(13, 10)).toBeUndefined()
  // Existing backend acquisition_progress_test.go records 720 / 1440 pages.
  expect(progressPercent(720, 1440)).toBe(50)
})
