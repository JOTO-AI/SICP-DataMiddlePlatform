import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { observationLabel, type SystemSnapshot } from '../src/app/overview'
test('log observation keeps missing backend fields unknown and never equates configuration with readiness', () => {
  const data = managementCapture('system').response.data as SystemSnapshot
  expect(observationLabel(data.platform?.storage_state)).toBe('未观测')
  expect(observationLabel(data.upstream?.buffer_state)).toBe('未观测')
  // Enum contract boundaries, not fabricated normal management responses.
  expect(observationLabel('available')).toBe('可读取')
  expect(observationLabel('unavailable')).toBe('不可读取')
  expect(observationLabel('future_state')).toBe('未知状态（future_state）')
})
