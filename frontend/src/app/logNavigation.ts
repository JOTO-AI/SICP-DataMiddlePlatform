import { shallowRef, watch } from 'vue'
import { session } from '../session'
import { sourceLogContext, type LogTaskContext } from './links'

// One bounded in-memory handoff; never put log filters in URLs or browser storage.
const pending = shallowRef<LogTaskContext | null>(null)
watch(() => session.epoch, () => { pending.value = null }, { flush: 'sync' })
export function prepareSourceLogs(id?: string | null): boolean {
  if (!session.operator) return false
  pending.value = sourceLogContext(id)
  return pending.value !== null
}
export function consumeSourceLogs(): LogTaskContext | null {
  const context = pending.value
  pending.value = null
  return context
}
