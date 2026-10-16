import { sourceTables } from '../acquisition'
import type { UpdateDetail } from './updateEvents'
export interface StageState { title: string; state: 'done' | 'active' | 'pending' | 'unknown' | 'skipped' | 'partial' | 'failed'; label: string; note?: string; at?: string | null }
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const buildPhases = ['building_table', 'validating_target', 'writing_table', 'validating_copy', 'content_check', 'business_check', 'index_check']
export function acquisitionSummary(task: UpdateDetail) {
  const raw = record(task.acquisition) ? task.acquisition.tables : undefined
  if (!Array.isArray(raw) || !raw.length || !raw.every(t => record(t) && typeof t.table === 'string' && typeof t.status === 'string' && sourceTables.includes(t.table) && ['waiting', 'checking', 'reusing', 'acquiring', 'collected', 'failed'].includes(String(t.status))) || new Set(raw.map(t => t.table)).size !== raw.length) return null
  return { total: raw.length, completed: raw.filter(t => t.status === 'collected').length, failed: raw.some(t => t.status === 'failed'), active: raw.some(t => ['checking', 'reusing', 'acquiring'].includes(t.status)), complete: raw.length === sourceTables.length && raw.every(t => t.status === 'collected') }
}
export function updateStageStates(task: UpdateDetail): StageState[] {
  const queued = ['queued', 'accepted'].includes(task.status)
  const stages: StageState[] = ['获取封存', '校验比较', '第一份构建', '查询转移', '排空', '第二份构建', '发布'].map(title => ({ title, state: queued ? 'pending' : 'unknown', label: queued ? '未开始' : '未记录阶段结果' }))
  const done = (index: number, note = '完成时间未单独记录') => { Object.assign(stages[index]!, { state: 'done', label: '已完成', note }) }
  const skip = (index: number, note: string) => { Object.assign(stages[index]!, { state: 'skipped', label: '不适用', note }) }
  const published = !!task.published_at || ['success_published', 'published_pending_progress'].includes(task.status)
  const unchanged = task.mode === 'incremental' && task.status === 'success_unchanged'
  const knownMode = ['initialize', 'incremental', 'force_full'].includes(task.mode)
  // These are workflow prerequisites, not invented stage timestamps or per-attempt results.
  // changed.go publishes only after both validated copies, confirmed shift and drain.
  // initialize.go builds both copies but has no route shift/drain; unchanged.go skips rebuild.
  if (knownMode && (published || unchanged)) { done(0); done(1) }
  if (knownMode && published) { done(2); done(5); if (task.mode !== 'initialize') { done(3); done(4) } }
  if (task.mode === 'initialize') { skip(3, '初始化无需转移查询'); skip(4, '初始化无需排空旧副本') }
  if (unchanged) for (const index of [2, 3, 4, 5, 6]) skip(index, '内容无变化，沿用现有数据')
  if (published) { done(6, task.published_at ? '' : '发布时间未记录'); stages[6]!.at = task.published_at }
  const acquisition = acquisitionSummary(task)
  if (stages[0]!.state !== 'done' && acquisition) {
    const count = acquisition.completed + ' / ' + acquisition.total + ' 张表完成'
    Object.assign(stages[0]!, { state: acquisition.complete ? 'partial' : acquisition.failed ? 'failed' : task.status === 'failed' ? 'unknown' : acquisition.active ? 'active' : 'pending', label: acquisition.complete ? '采集已完成' : count, note: acquisition.complete ? '封存状态待记录' : '' })
  }
  const work = record(task.work_progress) ? task.work_progress : null
  const phase = typeof work?.phase === 'string' ? work.phase : ''
  const running = ['running', 'published_pending_progress'].includes(task.status)
  const afterSeal = ['validating_input', ...buildPhases, 'checking_unchanged', 'draining', 'publishing']
  if (knownMode && running && afterSeal.includes(phase) && stages[0]!.state !== 'done') done(0)
  if (knownMode && running && [...buildPhases, 'draining', 'publishing'].includes(phase)) done(1)
  if (knownMode && running && phase === 'draining' && task.mode !== 'initialize') { done(2); done(3) }
  if (knownMode && running && phase === 'publishing') { done(2); done(5); if (task.mode !== 'initialize') { done(3); done(4) } }
  // Only a running activity can confirm its workflow prerequisites. Retained telemetry after failure is not success.
  let index = ['acquiring', 'sealing'].includes(phase) ? 0 : ['validating_input', 'checking_unchanged'].includes(phase) ? 1 : phase === 'draining' ? 4 : phase === 'publishing' ? 6 : -1
  if (buildPhases.includes(phase)) index = task.mode === 'initialize' && work?.instance === 'S' ? 5 : 2
  if (knownMode && running && index >= 0 && !['done', 'skipped'].includes(stages[index]!.state)) {
    Object.assign(stages[index]!, { state: 'active', label: phase === 'sealing' ? '封存中' : phase === 'acquiring' && acquisition?.complete ? '采集已完成' : buildPhases.includes(phase) ? '副本' + (work?.instance === 'M' || work?.instance === 'S' ? ' ' + work.instance : '') + '构建中' : '执行中', note: buildPhases.includes(phase) && task.mode !== 'initialize' ? '构建先后顺序未提供' : phase === 'acquiring' && acquisition?.complete ? '等待封存' : '' })
  }
  if (knownMode && running && buildPhases.includes(phase) && !published) {
    if (task.mode === 'initialize' && work?.instance === 'S') done(2)
    if (task.mode !== 'initialize') Object.assign(stages[2]!, { state: 'unknown', label: '副本构建中', note: '先后顺序未提供' })
  }
  return stages
}
export function operationGroupSummary(group: string, task?: UpdateDetail): string {
  if (!task) return '分组 · 展开查看记录'
  const stages = updateStageStates(task), acquisition = acquisitionSummary(task)
  if (group === 'acquisition') return stages[0]!.state === 'done' ? '采集与封存已完成' : acquisition ? acquisition.completed + ' / ' + acquisition.total + ' 张表完成' : stages[0]!.label
  if (group === 'builds' && ['running', 'published_pending_progress'].includes(task.status) && record(task.work_progress) && buildPhases.includes(String(task.work_progress.phase)) && !task.published_at && task.status !== 'published_pending_progress') return '副本构建中'
  if (group === 'builds') return stages[2]!.state === 'done' && stages[5]!.state === 'done' ? '两份副本构建已完成' : task.status === 'success_unchanged' ? '沿用现有副本' : [stages[2], stages[5]].find(s => s?.state === 'active')?.label ?? '分组 · 展开查看副本'
  if (group === 'publication') return stages[6]!.state === 'done' ? '数据已发布' : stages[6]!.state === 'skipped' ? '本次无需发布' : '分组 · 展开查看记录'
  if (group === 'progress') return task.progress_confirmed === true ? '同步进度已确认' : task.progress_confirmed === false ? '同步进度尚未提交' : '分组 · 展开查看记录'
  return group === 'trace' ? '诊断记录 · 展开查看' : '分组 · 展开查看记录'
}
