import { record, decimalString } from './notifications'
export const configurableRules = ['source_schema', 'task_failed', 'source_test_failed'] as const
export interface NotificationSettings { revision: string; enabled: boolean; recipients: string[]; rules: Record<string, boolean>; updated_at: string }
export interface NotificationSettingsView { settings: NotificationSettings; channel_state: 'configured' | 'unconfigured'; channel_diagnostics: { configured: boolean; last_result: string; observed_at: string | null } }
export interface NotificationDraft { enabled: boolean; recipients: string[]; rules: Record<string, boolean> }
export function normalizeRecipient(value: string): string {
  if (/[\r\n]/.test(value)) throw new Error('邮箱不能包含换行。')
  const address = value.trim(), at = address.lastIndexOf('@')
  if (new TextEncoder().encode(address).length > 254 || !/^[^\s@<>(),;:]+@[^\s@<>(),;:]+$/.test(address) || address.startsWith('.') || address.slice(0, at).endsWith('.') || address.includes('..')) throw new Error('请填写完整邮箱地址，不包含显示名称；最终格式由服务端校验。')
  return address.slice(0, at) + '@' + address.slice(at + 1).toLowerCase()
}
export function readNotificationSettings(value: unknown): NotificationSettings | null {
  if (!record(value) || !decimalString(value.revision) || value.revision === '0' || typeof value.enabled !== 'boolean' || !Array.isArray(value.recipients) || value.recipients.length > 20 || !value.recipients.every(v => typeof v === 'string' && v.length > 0 && new TextEncoder().encode(v).length <= 254) || !record(value.rules) || Object.keys(value.rules).some(k => !configurableRules.includes(k as typeof configurableRules[number]) || typeof (value.rules as Record<string, unknown>)[k] !== 'boolean') || typeof value.updated_at !== 'string' || !Number.isFinite(Date.parse(value.updated_at))) return null
  return value as unknown as NotificationSettings
}
export function readNotificationSettingsView(value: unknown): NotificationSettingsView | null {
  if (!record(value) || !readNotificationSettings(value.settings) || !['configured', 'unconfigured'].includes(String(value.channel_state)) || !record(value.channel_diagnostics)) return null
  const d = value.channel_diagnostics
  if (typeof d.configured !== 'boolean' || d.configured !== (value.channel_state === 'configured') || typeof d.last_result !== 'string' || !/^[a-z_]{1,64}$/.test(d.last_result) || d.observed_at !== null && (typeof d.observed_at !== 'string' || !Number.isFinite(Date.parse(d.observed_at)))) return null
  return value as unknown as NotificationSettingsView
}
export function notificationPayload(revision: string, draft: NotificationDraft, channelConfigured: boolean) {
  if (!decimalString(revision) || revision === '0' || draft.recipients.length > 20) throw new Error('配置版本无效或收件人超过 20 个。')
  const recipients = draft.recipients.map(normalizeRecipient)
  if (new Set(recipients).size !== recipients.length) throw new Error('收件人重复，请合并重复地址。')
  const enabled = recipients.length ? draft.enabled : false
  if (enabled && !channelConfigured) throw new Error('邮件通道未配置，当前只能保存关闭状态。')
  const rules = Object.fromEntries(configurableRules.map(key => [key, draft.rules[key] === true]))
  return { revision, enabled, recipients: recipients.sort(), rules }
}