import type { LogPage, LogSource } from './logHistory'
export function logStreamPath(source: LogSource, cursor: string): string {
  if (!['platform', 'upstream'].includes(source) || !cursor || cursor.length > 4096 || /[\r\n\x00]/.test(cursor)) throw new Error('实时日志游标无效，请重新读取实时起点。')
  return `/admin/v1/logs/${source}/stream?${new URLSearchParams({ cursor })}`
}
export function initialStreamPath(page: LogPage): string | null {
  if (page.filter.mode !== 'live' || !page.stream_cursor) return null
  try { return logStreamPath(page.source, page.stream_cursor) } catch { return null }
}export function reconnectDelay(attempt: number, retryAfter = 0): number {
  return Math.max(Math.min(30000, 1000 * 2 ** Math.min(Math.max(0, attempt), 5)), retryAfter)
}
export function streamFailureAction(status: number): 'retry' | 'rebuild' | 'stop' {
  if ([400, 409, 422].includes(status)) return 'rebuild'
  return status === 0 || status === 408 || status === 429 || status >= 500 ? 'retry' : 'stop'
}