export interface WorkProgress {
  phase: string; table?: string; tables?: string[]; instance?: string
  started_at: string; heartbeat_at: string; last_progress_at?: string
  unit?: 'rows' | 'cases'; detail?: string; completed?: number; total?: number; total_known?: boolean
  batches?: number; attempt?: number; cumulative_completed?: number; cumulative_elapsed_ms?: number
}
