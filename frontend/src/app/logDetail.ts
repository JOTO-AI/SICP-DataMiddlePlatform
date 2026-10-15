import { readLogEvent, logID, type LogEvent, type LogSource } from './logHistory'
export interface LogDetail extends LogEvent {
  entity_id: string; metadata: Record<string, unknown>
  response: { body?: string; request_id?: string; content_type?: string; truncated?: boolean; read_error?: boolean } | null
  redaction: { redacted: boolean; truncated: boolean; original_bytes: string; field_count: number }
}
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
export function readLogDetail(value: unknown, source: LogSource, id: string, epoch?: string, sequence?: string): LogDetail | null {
  if (!readLogEvent(value, source, epoch)) return null
  const d = value as LogDetail
  if (d.event_id !== id || sequence && d.sequence !== sequence || !logID(d.entity_id) || !object(d.metadata) || !object(d.redaction)) return null
  const r = d.redaction
  if (typeof r.redacted !== 'boolean' || typeof r.truncated !== 'boolean' || typeof r.original_bytes !== 'string' || !/^\d{1,19}$/.test(r.original_bytes) || !Number.isSafeInteger(r.field_count) || r.field_count < 0) return null
  if (d.response !== null) {
    if (!object(d.response)) return null
    const response: Record<string, unknown> = d.response
    if (['body', 'request_id', 'content_type'].some(k => response[k] !== undefined && typeof response[k] !== 'string') || ['truncated', 'read_error'].some(k => response[k] !== undefined && typeof response[k] !== 'boolean')) return null
  }
  return d
}
export const logKinds: Record<string, string> = { request: '接口请求', task: '更新任务', step: '更新步骤', call: '逻辑调用', attempt: '调用尝试', audit: '管理审计' }
export const logPhases: Record<string, string> = { started: '开始', finished: '结束', transport: '传输记录' }
export const logOutcomes: Record<string, string> = { succeeded: '成功', failed: '失败', interrupted: '中断' }