import { record, decimalString } from './notifications'
export interface Retention { revision: string; platform_days: number; upstream_days: number }
export const validDays = (value: unknown): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 3650
export function readRetention(value: unknown): Retention | null {
  return record(value) && decimalString(value.revision) && value.revision !== '0' && validDays(value.platform_days) && validDays(value.upstream_days) ? value as unknown as Retention : null
}
export function retentionPayload(revision: string, platform: string, upstream: string): Retention {
  const day = (text: string) => /^\d{1,4}$/.test(text) && validDays(Number(text)) ? Number(text) : null
  const platform_days = day(platform), upstream_days = day(upstream)
  if (!decimalString(revision) || revision === '0' || platform_days === null || upstream_days === null) throw new Error('保留天数必须为 1 至 3650 的整数，且需有效配置版本。')
  return { revision, platform_days, upstream_days }
}