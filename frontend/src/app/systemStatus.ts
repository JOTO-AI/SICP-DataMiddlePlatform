import { record, decimalString } from './notifications'
export type SystemStatus = Record<string, unknown>
export function readSystemStatus(value: unknown): SystemStatus | null {
  if (!record(value) || !['dual', 'single_legacy'].includes(String(value.log_storage_mode)) || !['available', 'unavailable'].includes(String(value.retention_state))) return null
  return value
}
export function statusObject(value: unknown): Record<string, unknown> { return record(value) ? value : {} }
export function statusCount(value: unknown): string {
  if (decimalString(value)) return value
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? String(value) : '未确认'
}
export function statusBoolean(value: unknown): string { return value === true ? '已就绪' : value === false ? '尚未就绪' : '未确认' }
export function statusConfigured(value: unknown): string { return value === true ? '已提供' : value === false ? '未提供' : '未确认' }
export function statusState(value: unknown): string {
  const labels: Record<string, string> = { available: '可用', unavailable: '不可用', unknown: '未知', complete: '本轮探测完成', incomplete: '本轮探测未完成' }
  return typeof value === 'string' && Object.hasOwn(labels, value) ? labels[value]! : '未确认'
}
export function statusTime(value: unknown): string | null { return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : null }