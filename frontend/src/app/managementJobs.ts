import { record, notificationID } from './notifications'
export interface ManagementJob { id: string; action: string; status: string; accepted_at: string; deadline: string; idempotency_expires_at: string; started_at: string | null; finished_at: string | null; summary: Record<string, unknown> }
export const terminalManagementJob = (job: ManagementJob | null) => !!job && ['succeeded', 'failed', 'interrupted', 'partial_failed'].includes(job.status)
export function readManagementJob(value: unknown, action: 'notification_test' | 'cleanup', id?: string): ManagementJob | null {
  const time = (v: unknown) => typeof v === 'string' && v.length <= 64 && Number.isFinite(Date.parse(v))
  if (!record(value) || !notificationID(value.id) || id && value.id !== id || value.action !== action || !['queued', 'running', 'succeeded', 'failed', 'interrupted', 'partial_failed'].includes(String(value.status)) || !['accepted_at', 'deadline', 'idempotency_expires_at'].every(k => time(value[k])) || !['started_at', 'finished_at'].every(k => value[k] === null || time(value[k])) || !record(value.summary)) return null
  return value as unknown as ManagementJob
}
export const notificationTestFact = (job: ManagementJob) => job.action === 'notification_test' && notificationID(job.id) ? 'explicit:' + job.id : null