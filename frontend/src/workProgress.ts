export interface WorkProgress {
  phase: string; table?: string; tables?: string[]; instance?: string
  started_at: string; heartbeat_at: string; last_progress_at?: string
  unit?: 'rows' | 'cases'; detail?: string; completed?: number; total?: number; total_known?: boolean
  batches?: number; attempt?: number; cumulative_completed?: number; cumulative_elapsed_ms?: number
}
// Counter fields in activity.Snapshot use omitempty: absent means zero.
// Null/unsafe/negative counters are not valid backend counters.
export function progressCounter(value: unknown): number | undefined {
  if (value === undefined) return 0
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined
}
export function progressPercent(completed: unknown, total: unknown, known = true): number | undefined {
  const done = progressCounter(completed), count = progressCounter(total)
  if (!known || done === undefined || count === undefined || count <= 0 || done > count) return undefined
  return Math.floor(done / count * 1000) / 10
}
