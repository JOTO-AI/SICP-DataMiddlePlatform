import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { logActivity } from '../src/app/logActivity'
import { readLogEvent } from '../src/app/logHistory'
import { managementCapture } from '../fixtures/backend'
const capture = JSON.parse(readFileSync(new URL('../fixtures/log-context.json', import.meta.url), 'utf8'))

test('timeline names backend-recorded start/end work and distinguishes physical copies', () => {
  for (const raw of [...capture.details, ...capture.items]) {
    const event = readLogEvent(raw, 'platform', 'epoch-1')!
    expect(event).not.toBeNull()
    const view = logActivity(event)
    expect(view.title).toBe('写入表数据 · ' + (raw.phase === 'started' ? '开始' : '结束'))
    expect(view.details).toContain('数据表：dwd_bzkscjzlb')
    expect(view.details).toContain('副本：' + raw.context.instance)
    expect(view.details).toContain('槽位：0')
    expect(view.details).toContain('第 1 次执行')
    expect(view.result).toBe(raw.phase === 'started' ? '已记录开始' : '成功')
  }
})
test('older details retain measured names and unknown names remain explicit', () => {
  const raw = structuredClone(capture.details[0])
  delete raw.context
  expect(logActivity(raw).title).toBe('写入表数据 · 开始')
  delete raw.metadata
  expect(logActivity(raw).title).toBe('更新步骤（名称未记录） · 开始')
  // Explicit protocol compatibility variants, not fabricated normal business data.
  raw.context = { stage: 'future_stage' }
  expect(logActivity(raw).title).toBe('future_stage · 开始')
  raw.context.stage = 'constructor'
  expect(logActivity(raw).title).toBe('constructor · 开始')
  const request = managementCapture('log_detail').response.data as any
  expect(logActivity(request).title).toBe('接口请求 · 开始')
})
test('malformed optional context fails closed while absent context remains compatible', () => {
  const raw = capture.items[0]
  for (const context of [[], 'write_table', { table: {} }, { stage: 'x'.repeat(129) }, { page: -1 }, { slot: 2 }, { attempt: 1.5 }, { stage: '\n' }]) {
    expect(readLogEvent({ ...raw, context }, 'platform')).toBeNull()
  }
  expect(readLogEvent({ ...raw, context: null }, 'platform')).not.toBeNull()
})
