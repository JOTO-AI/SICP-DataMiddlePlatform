import { shallowRef, watch } from 'vue'
import { session } from '../session'
import type { RosterContext } from './roster'
// One-use, session-bound memory handoff. Never share an interface snapshot or persist a roster.
const pending = shallowRef<RosterContext | null>(null)
watch(() => session.epoch, () => { pending.value = null }, { flush: 'sync' })
export function prepareRoster(context: RosterContext) {
  pending.value = session.operator ? { ...context, request: { ...context.request } } : null
  return pending.value !== null
}
export function consumeRoster() { const context = pending.value; pending.value = null; return context }