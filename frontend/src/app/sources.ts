export interface SourceState {
  id: string; primary_key: string; confirmed_until?: string | null; checked_at?: string | null
  task_id?: string | null; log_task_id?: string | null; window_task_id?: string | null
  // control.SourceWindow has no JSON tags: preserve the backend's exported field names.
  window?: { ConfirmedUntil?: string; Start?: string; End?: string } | null
  last_failure?: { task_id: string; log_task_id: string; code: string; observed_at: string } | null
  failure_state?: string
}
export const sourceFailureLabels: Record<string, string> = { none: '无失败记录', unresolved: '失败尚未覆盖', superseded_by_success: '失败已由成功检查覆盖' }
export function sourceItems(data: unknown): SourceState[] | null {
  if (!data || typeof data !== 'object' || !('items' in data) || !Array.isArray(data.items)) return null
  const ids = new Set<string>()
  for (const item of data.items) {
    if (!item || typeof item !== 'object' || typeof item.id !== 'string' || !item.id || ids.has(item.id)) return null
    ids.add(item.id)
  }
  return data.items
}
export function filterSources(items: SourceState[], name: string, state: string): SourceState[] {
  const term = name.trim().toLocaleLowerCase()
  return items.filter(item => item.id.toLocaleLowerCase().includes(term) && (!state || (item.failure_state ?? '') === state))
}
