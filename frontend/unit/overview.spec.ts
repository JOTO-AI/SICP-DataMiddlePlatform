import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { initializationState, actionReason, latestTaskTime, type RecentTask, type OverviewSnapshot, observationLabel, type SystemSnapshot } from '../src/app/overview'
test('log observation keeps missing backend fields unknown and never equates configuration with readiness', () => {
  const data = managementCapture('system').response.data as SystemSnapshot
  expect(observationLabel(data.platform?.storage_state)).toBe('未观测')
  expect(observationLabel(data.upstream?.buffer_state)).toBe('未观测')
  // Enum contract boundaries, not fabricated normal management responses.
  expect(observationLabel('available')).toBe('可读取')
  expect(observationLabel('unavailable')).toBe('不可读取')
  expect(observationLabel('future_state')).toBe('未知状态（future_state）')
})

test('overview event time selectors keep missing backend times absent', () => {
  const overview = managementCapture('overview').response.data as OverviewSnapshot
  expect(latestTaskTime(overview.tasks.items, 'published_at')).toBeUndefined()
  expect(latestTaskTime(overview.tasks.items, 'checked_at')).toBeUndefined()
  const task = managementCapture('task_detail').response.data as RecentTask
  // Use the recorded task unchanged: no manufactured event times or normal rows.
  expect(latestTaskTime([task], 'published_at')).toBe(task.published_at ?? undefined)
  expect(latestTaskTime([task], 'checked_at')).toBe(['success_published', 'success_unchanged'].includes(task.status) ? task.checked_at ?? undefined : undefined)
})

test('initialization respects the recorded empty environment and protocol state boundaries', () => {
  const overview = managementCapture('overview').response.data as OverviewSnapshot
  expect(initializationState(overview.tasks.initialized, false, overview.tasks.items[0]?.status)).toBe('waiting')
  expect(initializationState(false, true, 'failed')).toBe('unknown')
  expect(initializationState(undefined)).toBe('unknown')
  expect(initializationState(false, false, 'failed')).toBe('failed')
  expect(initializationState(false, false, 'published_pending_progress')).toBe('active')
  expect(initializationState(false, false, 'future_status')).toBe('unknown')
  expect(initializationState(true)).toBe('complete')
  expect(actionReason('task_or_publication_busy')).toContain('已有任务或发布')
  expect(actionReason(undefined)).toContain('后端未提供')
})
