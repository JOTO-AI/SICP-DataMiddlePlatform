export interface OverviewSnapshot {
  observed_at?: string
  tasks: { items: { task_id: string; mode: string; status: string; published_at?: string | null; checked_at?: string | null }[]; initialized: boolean; next_due_at?: string | null }
  active_state: string
  active: unknown
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
