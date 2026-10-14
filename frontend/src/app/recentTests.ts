import { readonly, shallowRef, watch } from 'vue'
import { session } from '../session'
import type { CatalogAPI } from './catalog'
import type { BusinessTestResult } from './businessTest'
import type { SourceJob, SourceTestRead } from './sourceTest'
import type { ApiError } from '../api/http'
import { businessOutcome, sourceOutcome, type TestOutcome } from './testOutcome'
import { operations } from '../queryOperations'
import { sourceQueryFields } from './sourceQueryFields'
export interface RecentTest {
  localID: number; apiId: string; kind: string; version: string; observedAt: string
  outcome: string; title: string; tone: TestOutcome['tone']
  httpStatus: number | null; requestID: string | null; durationMs: number | null
  jobID: string | null; jobStatus: string | null; resultState: string | null
}
const entries = shallowRef<Readonly<RecentTest>[]>([])
export const recentTests = readonly(entries)
export const recentTestLimit = 50
let sequence = 0
watch(() => session.epoch, clearRecentTests, { flush: 'sync' })
export function clearRecentTests() { entries.value = [] }
const identifier = (value: unknown) => typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value) ? value : null
const httpStatus = (value: unknown) => typeof value === 'number' && Number.isInteger(value) && value >= 100 && value <= 599 ? value : null
function record(api: CatalogAPI, outcome: TestOutcome, data: Pick<RecentTest, 'httpStatus' | 'requestID' | 'durationMs' | 'jobID' | 'jobStatus' | 'resultState'>, previous?: number): number | undefined {
  if (!session.operator || !(api.kind === 'business' ? operations.some(item => item.operation === api.id) : api.kind === 'source' && Object.hasOwn(sourceQueryFields, api.id))) return undefined
  const old = entries.value.find(entry => entry.localID === previous || data.jobID && entry.apiId === api.id && entry.jobID === data.jobID)
  const item = Object.freeze({ localID: old?.localID ?? ++sequence, apiId: api.id, kind: api.kind,
    version: /^[A-Za-z0-9_.:-]{1,128}$/.test(api.version) ? api.version : '未确认',
    observedAt: new Date().toISOString(), outcome: outcome.id, title: outcome.title, tone: outcome.tone, ...data })
  entries.value = [item, ...entries.value.filter(entry => entry.localID !== item.localID)].slice(0, recentTestLimit)
  return item.localID
}
export function recordBusinessTest(api: CatalogAPI, result: BusinessTestResult) {
  const response = result.response ?? result.error
  if (!response) return
  return record(api, businessOutcome({ busy: false, result }), {
    httpStatus: httpStatus(response.status), requestID: identifier(response.requestID),
    durationMs: Number.isSafeInteger(result.durationMs) && result.durationMs >= 0 ? result.durationMs : null,
    jobID: null, jobStatus: null, resultState: null,
  })
}
export function recordSourceTest(api: CatalogAPI, job?: SourceJob, response?: SourceTestRead, error?: ApiError, previous?: number) {
  const evidence = error ?? response
  return record(api, sourceOutcome({ busy: false, pending: true, job, response, error, waitingStopped: false }), {
    httpStatus: httpStatus(evidence?.status), requestID: identifier(evidence?.requestID), durationMs: null,
    jobID: identifier(job?.id), jobStatus: ['queued', 'running', 'succeeded', 'failed', 'interrupted', 'partial_failed'].includes(job?.status ?? '') ? job!.status : null,
    resultState: error ? 'unconfirmed' : response?.body.data.result_state === 'available' ? 'available' : response?.body.data.result_state === 'result_unavailable' ? 'result_unavailable' : 'unconfirmed',
  }, previous)
}