import { operations } from '../queryOperations'
export type LogSource = 'platform' | 'upstream'
export type IdentityField = 'request_id' | 'test_run_id' | 'task_id'
export interface LogFilters { source: LogSource; kind?: string; operation: string; identityField: IdentityField; identity: string; format: 'raw' | 'correlation'; outcome: string; from: string; to: string }
export const emptyLogFilters = (source: LogSource = 'platform'): LogFilters => ({ source, kind: '', operation: '', identityField: 'request_id', identity: '', format: 'raw', outcome: '', from: '', to: '' })
export const logID = (id: unknown): id is string => typeof id === 'string' && /^[a-f0-9]{64}$/.test(id)
const decimal = (v: unknown): v is string => typeof v === 'string' && /^(0|[1-9][0-9]*)$/.test(v) && v.length <= 19
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const time = (v: unknown): v is string => typeof v === 'string' && v.length <= 35 && Number.isFinite(Date.parse(v))
export function logHistoryPath(f: LogFilters, cursor = '') {
  if (!['platform', 'upstream'].includes(f.source)) throw new Error('日志源无效。')
  const q = new URLSearchParams()
  if (cursor) { if (cursor.length > 4096) throw new Error('日志游标过长。'); q.set('cursor', cursor) }
  else {
    q.set('mode', 'history'); q.set('page_size', '50')
    if (f.kind) { if (!(f.source === 'platform' ? ['request', 'task', 'step', 'audit'] : ['call', 'attempt']).includes(f.kind)) throw new Error('事件类型与日志来源不匹配。'); q.set('kind', f.kind) }
    if (f.operation) { if (!operations.some(op => op.operation === f.operation)) throw new Error('业务接口筛选无效。'); q.set('operation', f.operation) }
    if (f.identity) {
      if (!['request_id', 'test_run_id', 'task_id'].includes(f.identityField) || !['raw', 'correlation'].includes(f.format) || !(f.format === 'raw' ? /^[A-Za-z0-9_-]{1,128}$/.test(f.identity) : logID(f.identity))) throw new Error('请填写有效标识；原始 ID 使用字母、数字、下划线或连字符，关联 ID 为 64 位小写十六进制。')
      q.set(f.identityField, f.identity); q.set('id_format', f.format)
    }
    if (f.outcome) { if (!['succeeded', 'failed', 'interrupted'].includes(f.outcome)) throw new Error('结果筛选无效。'); q.set('outcome', f.outcome) }
    if (!!f.from !== !!f.to) throw new Error('请同时填写起止时间，或全部留空使用服务端最近 24 小时。')
    if (f.from && f.to) {
      const start = Date.parse(f.from), end = Date.parse(f.to)
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || end - start > 7 * 86400000) throw new Error('查询时间须递增，范围最多 7 天。')
      q.set('from', new Date(start).toISOString()); q.set('to', new Date(end).toISOString())
    }
  }
  return `/admin/v1/logs/${f.source}/events?${q}`
}
export interface LogEvent { source: LogSource; epoch: string; event_id: string; sequence: string; kind: string; phase: string; occurred_at: string | null; received_at: string | null; outcome: string | null; operation: string | null; request_id: string | null; task_id: string | null; http_status: number | null; duration_ns: string | null; test: { purpose: string; test_run_id: string } | null }
export interface LogPage { source: LogSource; epoch: string; high_water: string; retained_through: string; filter: Record<string, unknown>; items: LogEvent[]; next_cursor: string | null; expires_at: string | null }
export function readLogPage(value: unknown, f: LogFilters, previous?: LogPage): LogPage | null {
  if (!object(value)) return null
  const p = value as unknown as LogPage, filter = p.filter
  if (p.source !== f.source || typeof p.epoch !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(p.epoch) || !decimal(p.high_water) || !decimal(p.retained_through) || BigInt(p.retained_through) > BigInt(p.high_water) || !object(filter) || filter.mode !== 'history' || !time(filter.from) || !time(filter.to) || Date.parse(filter.from) >= Date.parse(filter.to) || filter.page_size !== 50 || !Array.isArray(p.items) || p.items.length > 50) return null
  if (p.next_cursor !== null && (typeof p.next_cursor !== 'string' || !p.next_cursor || p.next_cursor.length > 4096) || p.expires_at !== null && !time(p.expires_at)) return null
  if ((filter.operation ?? '') !== f.operation || (filter.outcome ?? '') !== f.outcome || (filter.kind ?? '') !== (f.kind ?? '') || filter.phase) return null
  for (const field of ['request_id', 'test_run_id', 'task_id'] as const) {
    const id = filter[field]
    if (f.identity && field === f.identityField ? !logID(id) || f.format === 'correlation' && id !== f.identity : id != null && id !== '') return null
  }
  if (f.from && (Date.parse(String(filter.from)) !== Date.parse(f.from) || Date.parse(String(filter.to)) !== Date.parse(f.to))) return null
  if (previous && (p.epoch !== previous.epoch || p.high_water !== previous.high_water || BigInt(p.retained_through) < BigInt(previous.retained_through) || ['mode', 'from', 'to', 'page_size', 'operation', 'outcome', 'request_id', 'test_run_id', 'task_id', 'kind', 'phase'].some(k => filter[k] !== previous.filter[k]))) return null
  const seen = new Set<string>(); let last = previous?.items.at(-1)?.sequence
  for (const item of p.items) {
    if (!object(item) || item.source !== p.source || item.epoch !== p.epoch || !logID(item.event_id) || seen.has(item.event_id) || !decimal(item.sequence) || BigInt(item.sequence) > BigInt(p.high_water) || BigInt(item.sequence) <= BigInt(p.retained_through) || last && BigInt(item.sequence) >= BigInt(last)) return null
    if (!['request', 'task', 'step', 'call', 'attempt', 'audit'].includes(item.kind) || !['started', 'finished', 'transport'].includes(item.phase) || item.outcome !== null && !['succeeded', 'failed', 'interrupted'].includes(item.outcome) || item.occurred_at !== null && !time(item.occurred_at) || item.received_at !== null && !time(item.received_at) || item.http_status !== null && (!Number.isInteger(item.http_status) || item.http_status < 100 || item.http_status > 599) || item.duration_ns !== null && !decimal(item.duration_ns)) return null
    if (f.kind && item.kind !== f.kind || f.source === 'upstream' && !['call', 'attempt'].includes(item.kind) || f.operation && item.operation !== f.operation || f.outcome && item.outcome !== f.outcome) return null
    for (const field of ['request_id', 'task_id'] as const) if (item[field] !== null && !logID(item[field])) return null
    if (item.test !== null && (!object(item.test) || !['test', 'sync'].includes(item.test.purpose) || item.test.purpose === 'test' && !logID(item.test.test_run_id) || item.test.purpose === 'sync' && item.test.test_run_id !== '')) return null
    if (f.identity && (f.identityField === 'test_run_id' ? item.test?.test_run_id : item[f.identityField]) !== filter[f.identityField]) return null
    seen.add(item.event_id); last = item.sequence
  }
  return p
}
