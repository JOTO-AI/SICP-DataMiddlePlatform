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

export function sourceFailureExplanation(state?: string): string {
  if (state === 'none') return '没有记录到采集失败，不代表源健康或检查成功。'
  if (state === 'unresolved') return '最近采集失败尚未被成功检查覆盖；失败不推进已确认进度。'
  if (state === 'superseded_by_success') return '后续成功检查已覆盖最近失败；失败记录仍保留，不据此推断当前健康。'
  return state ? `未知失败状态（${state}），请保留原码核对。` : '后端未提供失败状态，无法判断失败是否已被覆盖。'
}
