import type { CatalogAPI } from './catalog'
export interface CallEvent { source: string; event_id: string; sequence: string; kind: string; phase: string; occurred_at: string | null; outcome: string | null; operation: string | null; http_status: number | null; duration_ns: string | null }
export interface CallPage { source: string; items: CallEvent[]; filter: { mode: string; from: string; to: string; operation?: string; test_run_id?: string }; next_cursor: string | null; expires_at: string | null }
export function apiCallsPath(api: CatalogAPI, identity = '', format: 'raw' | 'correlation' = 'raw', cursor = ''): string {
  const params = new URLSearchParams()
  if (cursor) params.set('cursor', cursor)
  else {
    params.set('mode', 'history'); params.set('page_size', '50')
    if (api.kind === 'business') params.set('operation', api.id)
    else {
      if (!(format === 'raw' ? /^[A-Za-z0-9_-]{1,128}$/ : /^[a-f0-9]{64}$/).test(identity)) throw new Error(format === 'raw' ? '请填写该接口真实测试作业 ID（1–128 位字母、数字、下划线或连字符）。' : '日志关联 ID 须为 64 位小写十六进制。')
      params.set('test_run_id', identity); params.set('id_format', format)
    }
  }
  return `/admin/v1/logs/${api.kind === 'business' ? 'platform' : 'upstream'}/events?${params}`
}
export function readCallPage(value: unknown, api: CatalogAPI): CallPage | null {
  if (!value || typeof value !== 'object') return null
  const page = value as CallPage
  const source = api.kind === 'business' ? 'platform' : 'upstream'
  if (page.source !== source || !Array.isArray(page.items) || page.items.length > 200 || !page.filter || page.filter.mode !== 'history' || !Number.isFinite(Date.parse(page.filter.from)) || !Number.isFinite(Date.parse(page.filter.to))) return null
  if (api.kind === 'business' ? page.filter.operation !== api.id : !/^[a-f0-9]{64}$/.test(page.filter.test_run_id ?? '')) return null
  if (page.next_cursor !== null && (typeof page.next_cursor !== 'string' || !page.next_cursor || page.next_cursor.length > 4096)) return null
  const ids = new Set<string>()
  for (const item of page.items) {
    if (!item || item.source !== source || !/^[a-f0-9]{64}$/.test(item.event_id) || ids.has(item.event_id) || typeof item.sequence !== 'string' || !/^[0-9]+$/.test(item.sequence) || item.duration_ns != null && (typeof item.duration_ns !== 'string' || !/^[0-9]+$/.test(item.duration_ns))) return null
    ids.add(item.event_id)
  }
  return page
}
