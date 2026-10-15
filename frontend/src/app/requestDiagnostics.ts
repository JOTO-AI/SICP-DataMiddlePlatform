// One presentation contract for task and log evidence; legacy facts remain unknown when absent.
export interface ErrorResponse {
  body?: string; request_id?: string; content_type?: string
  truncated?: boolean; read_error?: boolean; unavailable?: string
}
export interface RequestEvidence {
  kind: 'data' | 'authentication'; endpoint?: string; method?: string
  started_at?: string; duration_ns?: number; sent?: boolean; http_status?: number
  business_code?: string; stage?: string; failure?: string
  error?: string; error_type?: string; error_truncated?: boolean; response?: ErrorResponse
}
export interface EvidenceInput {
  diagnostics?: unknown; response?: unknown; authentication?: unknown
  http_status?: unknown; business_code?: unknown; path?: unknown
}
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const text = (v: unknown, max: number) => typeof v === 'string' && v.length <= max
const status = (v: unknown): v is number => Number.isInteger(v) && Number(v) >= 100 && Number(v) <= 599
function response(v: unknown): v is ErrorResponse {
  return record(v) && ['body', 'request_id', 'content_type', 'unavailable'].every(k => v[k] === undefined || text(v[k], k === 'body' ? 2048 : 256)) && ['truncated', 'read_error'].every(k => v[k] === undefined || typeof v[k] === 'boolean')
}
function request(v: unknown): v is RequestEvidence {
  if (!record(v) || (v.kind !== 'data' && v.kind !== 'authentication') || v.method !== 'GET' || !text(v.endpoint, 512) || !v.endpoint) return false
  if (typeof v.started_at !== 'string' || !Number.isFinite(Date.parse(v.started_at)) || typeof v.duration_ns !== 'number' || !Number.isFinite(v.duration_ns) || !Number.isInteger(v.duration_ns) || v.duration_ns < 0 || typeof v.sent !== 'boolean') return false
  if (v.http_status !== undefined && (!status(v.http_status) || !v.sent)) return false
  if (!text(v.stage, 64) || !v.stage || !text(v.failure, 64) || !v.failure) return false
  return ['business_code', 'error', 'error_type'].every(k => v[k] === undefined || text(v[k], k === 'error' ? 512 : 128)) && (v.error_truncated === undefined || typeof v.error_truncated === 'boolean') && (v.response === undefined || response(v.response))
}
export function requestDiagnostics(input: EvidenceInput): { items: RequestEvidence[]; malformed: boolean } {
  if (input.diagnostics !== undefined) {
    if (!Array.isArray(input.diagnostics)) return { items: [], malformed: true }
    const items = input.diagnostics.slice(0, 2).filter(request)
    return { items, malformed: items.length !== input.diagnostics.length }
  }
  // Historical events do not provide request start time, duration or sent flag.
  const items: RequestEvidence[] = []
  let malformed = false
  if (input.response != null || input.http_status != null || input.business_code != null) {
    malformed ||= input.response != null && !response(input.response) || input.http_status != null && !status(input.http_status)
    items.push({ kind: 'data', endpoint: typeof input.path === 'string' ? input.path : undefined,
      http_status: status(input.http_status) ? input.http_status : undefined,
      business_code: typeof input.business_code === 'number' && Number.isSafeInteger(input.business_code) ? String(input.business_code) : undefined,
      response: response(input.response) ? input.response : undefined })
  }
  if (input.authentication != null) {
    const a = input.authentication
    if (!record(a) || !text(a.endpoint, 512) || !text(a.reason, 64)) malformed = true
    else {
      malformed ||= a.http_status !== undefined && !status(a.http_status) || a.response !== undefined && !response(a.response)
      items.push({ kind: 'authentication', endpoint: a.endpoint as string, failure: a.reason as string,
        http_status: status(a.http_status) ? a.http_status : undefined, response: response(a.response) ? a.response : undefined })
    }
  }
  return { items, malformed }
}
