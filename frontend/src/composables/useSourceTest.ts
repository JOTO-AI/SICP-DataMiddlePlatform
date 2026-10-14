import { computed, ref, shallowRef } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { ApiError } from '../api/http'
import { submitOperation, type PendingOperation } from '../api/operations'
import { createSourceTest, readSourceTest, terminalSourceJob, validSourceJob, waitForSourcePoll, type SourceJob, type SourceTestRead } from '../app/sourceTest'
import { loadPendingRecovery, savePendingRecovery, removePendingRecovery } from '../storage/pending'
import { session } from '../session'
import { recordSourceTest } from '../app/recentTests'
import { useRequestScope } from './useRequestScope'
export function useSourceTest(api: CatalogAPI) {
  const context = { environment: window.location.origin + import.meta.env.BASE_URL, subject: session.operator }
  const recovered = api.kind === 'source' ? loadPendingRecovery(context).find(entry => entry.operation.path === '/admin/v1/api-tests' && JSON.parse(entry.operation.payload).api_id === api.id) : undefined
  const operation = shallowRef<PendingOperation | null>(recovered?.operation ?? null), location = ref(recovered?.location ?? '')
  const job = shallowRef<SourceJob>(), response = shallowRef<SourceTestRead>(), error = shallowRef<ApiError>()
  const busy = ref(false), waitingStopped = ref(false), recoveryAvailable = ref(true), rejected = ref(false)
  const scope = useRequestScope(), terminal = computed(() => terminalSourceJob(job.value))
  const locked = computed(() => !!operation.value || busy.value)
  let summaryID: number | undefined
  function summarize(currentResponse?: SourceTestRead, currentError?: ApiError) {
    if (!operation.value) return
    const fixed = JSON.parse(operation.value.payload)
    summaryID = recordSourceTest({ ...api, version: fixed.version }, job.value, currentResponse, currentError, summaryID)
  }
  function remember() { if (operation.value) recoveryAvailable.value = savePendingRecovery(context, operation.value, location.value || undefined) }
  function stop() { scope.cancel(); busy.value = false }
  function reset() {
    if (busy.value || !terminal.value && !rejected.value) return
    if (operation.value) removePendingRecovery(context, operation.value)
    summaryID = undefined
    operation.value = null; location.value = ''; job.value = undefined; response.value = undefined; error.value = undefined; waitingStopped.value = false; rejected.value = false
  }
  async function execute(text?: string) {
    if (busy.value || api.kind !== 'source') return
    busy.value = true; error.value = undefined; waitingStopped.value = false; rejected.value = false
    const run = scope.begin()
    try {
      if (!operation.value) operation.value = createSourceTest(api, text ?? '{}')
      const submission = operation.value
      remember()
      if (!location.value) {
        const accepted = await submitOperation<SourceJob>(submission, run.signal)
        if (!run.current()) return
        location.value = accepted.location; remember()
        if (!validSourceJob(accepted.body.data, location.value.split('/').at(-1)!)) throw new ApiError('受理回执缺少有效作业状态，请按作业位置查询。', 'invalid_response')
        job.value = accepted.body.data; summarize()
      }
      // A click starts at most one POST and a bounded GET chain. No retry after read errors.
      const until = performance.now() + 30000
      for (let attempt = 0; attempt < 15 && run.current(); attempt++) {
        const remaining = until - performance.now()
        if (remaining <= 0) break
        const next = await readSourceTest(submission, location.value, run.signal, Math.min(10000, Math.max(1, remaining)))
        if (!run.current()) return
        response.value = next; job.value = next.body.data.job; summarize(next)
        if (terminal.value) { removePendingRecovery(context, submission); return }
        if (attempt < 14) await waitForSourcePoll(run.signal, Math.min(2000, Math.max(1, until - performance.now())))
      }
      if (run.current()) waitingStopped.value = true
    } catch (cause) {
      if (!run.current()) return
      error.value = cause instanceof ApiError ? cause : new ApiError(cause instanceof Error ? cause.message : '源测试失败。', 'invalid_argument')
      summarize(undefined, error.value)
      // Only definitive admission refusals allow editing a fresh request. Transport uncertainty keeps the exact key/payload.
      rejected.value = !location.value && [400, 403, 404, 409, 413, 429].includes(error.value.status) && !['invalid_response', 'network_error', 'timeout', 'cancelled'].includes(error.value.code)
      if (rejected.value && operation.value) removePendingRecovery(context, operation.value)
    } finally { if (run.current()) busy.value = false }
  }
  return { operation, location, job, response, error, busy, waitingStopped, recoveryAvailable, terminal, locked, rejected, execute, stop, reset }
}
