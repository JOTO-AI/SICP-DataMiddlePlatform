import { test, expect } from '@playwright/test'
import { recordedLogPage } from '../fixtures/logTests'
import { emptyLogBuffer, appendLog, revealLogs, trimLogBuffer } from '../src/app/logBuffer'
test('visible, pending and reserved history stay inside the shared summary budget without automatic reveal', () => {
  const item = recordedLogPage().data.items[0], buffer = emptyLogBuffer()
  // Repeated references test capacity only; stream identity deduplication is tested separately.
  for (let i = 0; i < 1200; i++) appendLog(buffer, item, 950)
  expect(buffer.received).toBe(1200); expect(buffer.visible).toEqual([])
  expect(buffer.pending.length + 50).toBe(1000); expect(buffer.dropped).toBe(250)
  revealLogs(buffer, 950); const first = buffer.visible[0]
  expect(buffer.visible).toHaveLength(475)
  for (let i = 0; i < 1000; i++) appendLog(buffer, item, 950)
  expect(buffer.visible[0]).toBe(first)
  expect(buffer.visible.length + buffer.pending.length + 50).toBe(1000)
  trimLogBuffer(buffer, 1); revealLogs(buffer, 1); expect(buffer.visible).toHaveLength(1)
  trimLogBuffer(buffer, 0); expect(buffer.pending).toEqual([]); expect(buffer.visible).toEqual([])
})