import { record, decimalString, notificationID } from './notifications'
export interface CleanupPreview { id: string; source: 'platform' | 'upstream'; revision: string; epoch: string; floor: string; through: string; count: string; cutoff: string; expires_at: string; protection: string }
export function readCleanupPreview(value: unknown, source: string): CleanupPreview | null {
  if (!record(value) || !['platform', 'upstream'].includes(source) || value.source !== source || !notificationID(value.id) || !['revision', 'floor', 'through', 'count'].every(key => decimalString(value[key])) || value.revision === '0' || typeof value.epoch !== 'string' || !/^[a-f0-9-]{36}$/.test(value.epoch) || !['cutoff', 'expires_at'].every(key => typeof value[key] === 'string' && Number.isFinite(Date.parse(value[key] as string))) || value.protection !== 'durable_reference_retirement') return null
  if (BigInt(value.floor as string) > BigInt(value.through as string)) return null
  return value as unknown as CleanupPreview
}
export function previewReason(preview: CleanupPreview | null, revision: string, now: number): string {
  if (!preview) return '请先预览所选日志源。'
  if (Date.parse(preview.expires_at) <= now) return '预览已过期，请重新预览。'
  if (revision && preview.revision !== revision) return '保留策略版本已变化，请重新预览。'
  return ''
}
import { objectTokens } from '../shared/jsonObject'
import { terminalManagementJob, type ManagementJob } from './managementJobs'
export function cleanupResult(job: ManagementJob, raw?: string, expectedSource?: string): { source: 'platform' | 'upstream'; deleted: string; receipt: string } | null {
  const summary = job.summary
  if (!terminalManagementJob(job) || summary.outcome_known !== true || !['platform', 'upstream'].includes(String(summary.source)) || expectedSource && summary.source !== expectedSource || summary.receipt_id !== job.id) return null
  let deleted: unknown = typeof summary.deleted === 'number' && Number.isSafeInteger(summary.deleted) && summary.deleted >= 0 ? String(summary.deleted) : null
  if (raw) {
    try { const data = objectTokens(raw).get('data'), body = data && objectTokens(data).get('summary'); deleted = body ? objectTokens(body).get('deleted') : null } catch { return null }
  }
  if (!decimalString(deleted)) return null
  return { source: summary.source as 'platform' | 'upstream', deleted, receipt: job.id }
}