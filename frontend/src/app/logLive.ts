import type { LogPage, LogSource } from './logHistory'
export function logStreamPath(source: LogSource, cursor: string): string {
  if (!['platform', 'upstream'].includes(source) || !cursor || cursor.length > 4096 || /[\r\n\x00]/.test(cursor)) throw new Error('实时日志游标无效，请重新读取实时起点。')
  return `/admin/v1/logs/${source}/stream?${new URLSearchParams({ cursor })}`
}
export function initialStreamPath(page: LogPage): string | null {
  if (page.filter.mode !== 'live' || !page.stream_cursor) return null
  try { return logStreamPath(page.source, page.stream_cursor) } catch { return null }
}