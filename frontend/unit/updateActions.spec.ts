import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { readActionReasons, actionAllows } from '../src/app/updateActions'
test('update action advice preserves captured empty reasons and rejects unavailable or malformed snapshots', () => {
  const reasons = readActionReasons(managementCapture('overview').response.data)
  expect(reasons).toEqual({}); expect(actionAllows(reasons, 'incremental')).toBe(true)
  expect(actionAllows(null, 'incremental')).toBe(false)
  expect(readActionReasons({ actions: [] })).toBeNull()
  expect(readActionReasons({ actions: { incremental: false } })).toBeNull()
  for (const reason of ['task_or_publication_busy', 'active_unavailable', 'initialization_required', 'future_reason']) expect(actionAllows({ incremental: reason }, 'incremental')).toBe(false)
  expect(actionAllows({ rerun: 'failed_task_selection_required' }, 'rerun')).toBe(true)
  expect(actionAllows({ force_full: 'failed_task_selection_required' }, 'force_full')).toBe(false)
})