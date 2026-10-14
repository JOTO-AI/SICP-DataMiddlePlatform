export interface RecentTask { task_id: string; mode: string; status: string; accepted_at?: string; started_at?: string | null; finished_at?: string | null; published_at?: string | null; checked_at?: string | null }
export interface OverviewSnapshot {
  observed_at?: string
  tasks: { items: RecentTask[]; initialized: boolean; next_due_at?: string | null }
  active_state: string
  active: { event_id?: string; task_id?: string } | null
  slots: unknown[]
  actions: Record<string, string>
}
export interface LogObservation { reader_configured?: boolean; storage_state?: string; buffer_state?: string }
export interface SystemSnapshot { observed_at?: string; platform?: LogObservation | null; upstream?: LogObservation | null }
export function observationLabel(state: unknown): string {
  if (state === 'available') return '可读取'
  if (state === 'unavailable') return '不可读取'
  if (state === 'unknown' || state == null) return '未观测'
  return typeof state === 'string' ? `未知状态（${state}）` : '未观测'
}

// Select only actual backend event times, never accepted_at or the browser clock.
export function latestTaskTime(items: RecentTask[], field: 'published_at' | 'checked_at'): string | undefined {
  return items.filter(item => field === 'published_at' || ['success_published', 'success_unchanged'].includes(item.status))
    .map(item => item[field]).filter((value): value is string => !!value && !Number.isNaN(Date.parse(value)))
    .sort((a, b) => Date.parse(b) - Date.parse(a))[0]
}
export const taskModeLabels: Record<string, string> = { initialize: '初始化', incremental: '增量同步', force_full: '强制全量', table_force_full: '单表强制更新' }

export const actionReasonLabels: Record<string, string> = {
  available_recheck_on_submit: '可进入维护页面核对，提交时后端会再次检查。',
  initialization_required: '首次初始化完成前不可执行。',
  failed_initialization_required: '仅在失败的初始化任务允许补齐时执行。',
  task_or_publication_busy: '已有任务或发布正在处理，请先查看当前进度。',
  failed_task_selection_required: '需在更新记录中选择具体失败任务。',
  active_unavailable: '当前数据组合无法确认，请先恢复状态读取。',
  active_slot_unavailable: '当前数据槽位不可用，请先查看系统状态。',
  active_empty: '尚无已发布的数据组合。',
}
export function actionReason(reason?: string): string {
  return reason ? actionReasonLabels[reason] ?? `后端限制：${reason}` : '后端未提供可执行条件，进入维护页后需重新核对。'
}
export function initializationState(initialized?: boolean, stale = false, latestStatus?: string): 'unknown' | 'complete' | 'failed' | 'active' | 'waiting' {
  if (stale || initialized == null) return 'unknown'
  if (initialized) return 'complete'
  if (latestStatus === 'failed') return 'failed'
  if (latestStatus && ['queued', 'accepted', 'running', 'published_pending_progress'].includes(latestStatus)) return 'active'
  if (latestStatus && !['success_published', 'success_unchanged'].includes(latestStatus)) return 'unknown'
  return 'waiting'
}
