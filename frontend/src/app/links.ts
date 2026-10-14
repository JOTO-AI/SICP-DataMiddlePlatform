// Event IDs are lower-case 64-digit hex correlation IDs in logquery.Detail.
// Never hash, decode, or substitute an originating request/task ID here.
export function logDetailPath(source: string, eventId: string): string | null {
  if (!['platform', 'upstream'].includes(source) || !/^[a-f0-9]{64}$/.test(eventId)) return null
  return `/logs/${source}/events/${eventId}`
}

export function updateDetailPath(taskId?: string | null): string | null {
  if (!taskId || taskId.length > 128 || !/^[A-Za-z0-9_:-]+$/.test(taskId)) return null
  return `/updates/${encodeURIComponent(taskId)}`
}
export interface LogTaskContext { source: 'upstream'; task_id: string; id_format: 'correlation' }
export function sourceLogContext(logTaskId?: string | null): LogTaskContext | null {
  if (!logTaskId || !/^[a-f0-9]{64}$/.test(logTaskId)) return null
  return { source: 'upstream', task_id: logTaskId, id_format: 'correlation' }
}
