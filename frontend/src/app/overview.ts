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
export interface SourceState {
  id: string; primary_key: string; confirmed_until?: string | null; checked_at?: string | null
  task_id?: string | null; log_task_id?: string | null; window_task_id?: string | null
  window?: { from?: string | null; to?: string | null } | null
  last_failure?: { task_id: string; log_task_id: string; code: string; observed_at: string } | null
  failure_state?: string
}
export const sourceFailureLabels: Record<string, string> = { none: '无失败记录', unresolved: '失败尚未覆盖', superseded_by_success: '失败已由成功检查覆盖' }
