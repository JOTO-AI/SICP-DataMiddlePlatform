import { emptyLogFilters, logID, readLogEvent, type LogEvent, type LogFilters } from './logHistory'
// Time only bounds the scan. Membership always requires the same source, epoch and call_id.
export function attemptFilters(call: LogEvent): LogFilters | null {
  if (call.source !== 'upstream' || call.kind !== 'call' || !logID(call.call_id) || !call.occurred_at) return null
  const identityField = call.test?.test_run_id ? 'test_run_id' : call.task_id ? 'task_id' : call.request_id ? 'request_id' : null
  if (!identityField) return null
  const identity = identityField === 'test_run_id' ? call.test!.test_run_id : call[identityField]!
  const at = Date.parse(call.occurred_at)
  if (!Number.isFinite(at)) return null
  return { ...emptyLogFilters('upstream'), kind: 'attempt', identityField, identity, format: 'correlation', from: new Date(Math.max(Date.UTC(2000, 0, 1), at - 86400000)).toISOString(), to: new Date(Math.min(Date.UTC(9999, 11, 31), at + 86400000)).toISOString() }
}
export function belongsToCall(item: LogEvent, call: LogEvent): boolean {
  return !!readLogEvent(item, 'upstream', call.epoch) && call.source === 'upstream' && call.kind === 'call' && logID(call.call_id) && item.kind === 'attempt' && item.call_id === call.call_id && logID(item.attempt_id) && item.attempt_no !== null
}