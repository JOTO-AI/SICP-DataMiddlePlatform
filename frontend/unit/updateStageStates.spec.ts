import { test, expect } from '@playwright/test'
import { managementCapture, backendCatalog } from '../fixtures/backend'
import { readUpdateDetail, type UpdateDetail } from '../src/app/updateEvents'
import { acquisitionSummary, operationGroupSummary, updateStageStates } from '../src/app/updateStageStates'
// Contract-state variations of the recorded DTO, not new business rows or invented timestamps.
const captured = managementCapture('update_detail').response.data as UpdateDetail
const task = (fields: Partial<UpdateDetail> = {}) => ({ ...captured, ...fields })
const tables = (backendCatalog() as { id: string; kind: string }[]).filter(api => api.kind === 'source').map(api => ({ table: api.id, status: 'collected' }))

test('captured queued detail remains unstarted and its grouping node is not an unknown execution', () => {
  expect(readUpdateDetail(captured, captured.task_id)).not.toBeNull()
  expect(updateStageStates(captured).every(stage => stage.state === 'pending')).toBe(true)
  expect(operationGroupSummary('publication', captured)).toBe('分组 · 展开查看记录')
})

test('completed acquisition from backend catalog does not imply sealing or publication', () => {
  expect(tables).toHaveLength(14)
  const value = task({ status: 'running', acquisition: { tables } })
  expect(acquisitionSummary(value)?.complete).toBe(true)
  expect(updateStageStates(value)[0]).toMatchObject({ state: 'partial', label: '采集已完成', note: '封存状态待记录' })
  expect(updateStageStates(value)[6]!.state).not.toBe('done')
  expect(operationGroupSummary('acquisition', value)).toBe('14 / 14 张表完成')
  for (const invalid of [[], tables.map(t => ({ ...t, table: [t.table] })), [tables[0]], [...tables, tables[0]], [{ table: 'not-in-catalog', status: 'collected' }]]) {
    expect(acquisitionSummary(task({ acquisition: { tables: invalid } }))?.complete).not.toBe(true)
  }
})

test('published changed workflow proves prerequisites without manufacturing individual times', () => {
  for (const mode of ['incremental', 'force_full']) for (const status of ['success_published', 'published_pending_progress']) {
    const value = task({ mode, status, progress_confirmed: false })
    const stages = updateStageStates(value)
    expect(stages.every(s => s.state === 'done')).toBe(true)
    expect(stages.every(s => !s.at)).toBe(true)
    expect(operationGroupSummary('builds', value)).toBe('两份副本构建已完成')
    expect(operationGroupSummary('progress', value)).toBe('同步进度尚未提交')
  }
})

test('initialization and unchanged workflows skip steps they never execute', () => {
  const initial = updateStageStates(task({ mode: 'initialize', status: 'success_published' }))
  expect(initial.map(s => s.state)).toEqual(['done', 'done', 'done', 'skipped', 'skipped', 'done', 'done'])
  const unchanged = updateStageStates(task({ mode: 'incremental', status: 'success_unchanged' }))
  expect(unchanged.map(s => s.state)).toEqual(['done', 'done', 'skipped', 'skipped', 'skipped', 'skipped', 'skipped'])
})

test('current activity and failed retained telemetry do not fabricate completion', () => {
  const work_progress = { phase: 'writing_table', instance: 'S' }
  const active = updateStageStates(task({ mode: 'initialize', status: 'running', work_progress }))
  expect(active[0]!.state).toBe('done')
  expect(active[1]!.state).toBe('done')
  const draining = updateStageStates(task({ mode: 'incremental', status: 'running', work_progress: { phase: 'draining' } }))
  expect(draining.slice(0, 4).every(s => s.state === 'done')).toBe(true)
  expect(draining[4]!.state).toBe('active')
  expect(draining[5]!.state).not.toBe('done')
  expect(active[5]!.state).toBe('active')
  const failed = updateStageStates(task({ status: 'failed', work_progress }))
  expect(failed.some(s => s.state === 'done' || s.state === 'active')).toBe(false)
  const unknown = updateStageStates(task({ mode: 'future-mode', status: 'success_published' }))
  expect(unknown.filter(s => s.state === 'done')).toHaveLength(1)
})
