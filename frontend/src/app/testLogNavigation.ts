import { shallowRef, watch } from 'vue'
import { session } from '../session'
import { emptyLogFilters, logHistoryPath, type LogFilters } from './logHistory'
const pending = shallowRef<LogFilters | null>(null)
watch(() => session.epoch, () => { pending.value = null }, { flush: 'sync' })
export function testLogFilters(kind: string, requestID?: string | null, jobID?: string | null): LogFilters | null {
  const identity = kind === 'business' ? requestID : kind === 'source' ? jobID : null
  if (!identity) return null
  const filters = { ...emptyLogFilters(kind === 'business' ? 'platform' : 'upstream'), identity, identityField: kind === 'business' ? 'request_id' as const : 'test_run_id' as const }
  try { logHistoryPath(filters); return filters } catch { return null }
}
export function prepareTestLogs(filters: LogFilters) { pending.value = session.operator ? { ...filters } : null; return pending.value !== null }
export function consumeTestLogs() { const value = pending.value; pending.value = null; return value }
