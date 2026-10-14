import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { filterSources, sourceItems } from '../src/app/sources'

test('source filters preserve the backend example and cannot invent missing rows', () => {
  const data = managementCapture('sources').response.data
  const items = sourceItems(data)!
  expect(items).toHaveLength(1) // selected_items capture, not a fictional full 14-source snapshot
  expect(filterSources(items, ` ${items[0]!.id.toUpperCase()} `, 'none')).toEqual(items)
  expect(filterSources(items, '', 'unresolved')).toEqual([])
  expect(filterSources(items, 'no matching source', '')).toEqual([])
  expect(items[0]!.confirmed_until).toBeNull()
  expect(items[0]!.window).toBeNull()
  expect(sourceItems(null)).toBeNull()
  expect(sourceItems({ items: [items[0], items[0]] })).toBeNull()
})

import { sourceFailureExplanation } from '../src/app/sources'
import { updateDetailPath, sourceLogContext } from '../src/app/links'
import { prepareSourceLogs, consumeSourceLogs } from '../src/app/logNavigation'
import { clearSession, session } from '../src/session'

test('source failure meanings distinguish control evidence without inferring health', () => {
  expect(sourceFailureExplanation('none')).toContain('不代表源健康')
  expect(sourceFailureExplanation('unresolved')).toContain('失败不推进已确认进度')
  expect(sourceFailureExplanation('superseded_by_success')).toContain('失败记录仍保留')
  expect(sourceFailureExplanation('new_state')).toContain('未知失败状态（new_state）')
  expect(sourceFailureExplanation(undefined)).toContain('无法判断')
})

test('control navigation and correlated log handoff preserve distinct ID contracts and clear on logout', () => {
  const task = managementCapture('task_detail').response.data as { task_id: string }
  // The recorded log event supplies a canonical digest for an ID-format boundary test,
  // not a fabricated source/task response or a claim that this event is a task.
  const digest = (managementCapture('log_detail').response.data as { event_id: string }).event_id
  expect(updateDetailPath(task.task_id)).toBe(`/updates/${task.task_id}`)
  expect(sourceLogContext(task.task_id)).toBeNull()
  expect(sourceLogContext(digest)).toEqual({ source: 'upstream', task_id: digest, id_format: 'correlation' })
  expect(sourceLogContext(digest.toUpperCase())).toBeNull()
  expect(updateDetailPath('../logs')).toBeNull()
  expect(updateDetailPath('task:1')).toBe('/updates/task%3A1') // operations.ValidID allows colon
  clearSession()
  session.operator = 'fixture-reviewer'
  expect(prepareSourceLogs(digest)).toBe(true)
  expect(consumeSourceLogs()?.task_id).toBe(digest)
  expect(consumeSourceLogs()).toBeNull()
  expect(prepareSourceLogs(digest)).toBe(true)
  clearSession()
  expect(consumeSourceLogs()).toBeNull()
  expect(prepareSourceLogs(digest)).toBe(false)
})
