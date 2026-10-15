export const notificationRules: Record<string, string> = { source_schema: '源结构变化', task_failed: '更新任务失败', source_test_failed: '源测试失败', test: '测试邮件' }
export const deliveryStatuses: Record<string, string> = { queued: '等待发送', retry: '等待重试', sending: '发送中', accepted: '通道已接受', rejected: '通道已拒绝', unknown: '发送结果未知', cancelled: '已取消待发送', exhausted: '重试已耗尽' }
export const decisionStatuses: Record<string, string> = { disabled: '通知或该规则未启用', queued: '已创建投递', legacy: '历史决策' }
export const notificationID = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9_:-]{1,128}$/.test(v)
export const decimalString = (v: unknown): v is string => typeof v === 'string' && /^(0|[1-9]\d{0,18})$/.test(v) && BigInt(v) <= 9223372036854775807n
export const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const time = (v: unknown) => typeof v === 'string' && v.length <= 64 && Number.isFinite(Date.parse(v))
const word = (v: unknown) => typeof v === 'string' && /^[a-z_]{1,64}$/.test(v)
export interface NotificationDecision { id: string; code: string; decision: string; revision: string; occurred_at: string; deliveries: string; accepted: string; pending: string; unknown: string }
export interface NotificationDelivery { id: string; fact_id: string; rule_id: string; revision: string; recipient: string; status: string; attempts: number; created_at: string; deadline: string; next_attempt_at: string | null; lease_until: string | null; accepted_at: string | null }
export interface DeliveryAttempt { number: number; started_at: string; finished_at: string | null; outcome: string }
export interface DeliveryDetail { delivery: NotificationDelivery; attempts: DeliveryAttempt[] }
export type NotificationKind = 'decisions' | 'deliveries'
export interface NotificationPage { items: (NotificationDecision | NotificationDelivery)[]; next_cursor: string | null }
export function readDecision(v: unknown): NotificationDecision | null {
  if (!record(v) || !notificationID(v.id) || !word(v.code) || !word(v.decision) || !decimalString(v.revision) || v.revision === '0' || !time(v.occurred_at) || !['deliveries', 'accepted', 'pending', 'unknown'].every(k => decimalString(v[k]))) return null
  if (BigInt(v.accepted as string) + BigInt(v.pending as string) + BigInt(v.unknown as string) > BigInt(v.deliveries as string)) return null
  return v as unknown as NotificationDecision
}
export function readDelivery(v: unknown): NotificationDelivery | null {
  if (!record(v) || !notificationID(v.id) || !notificationID(v.fact_id) || !word(v.rule_id) || !decimalString(v.revision) || v.revision === '0' || typeof v.recipient !== 'string' || !v.recipient || v.recipient.length > 254 || !word(v.status) || !Number.isSafeInteger(v.attempts) || Number(v.attempts) < 0 || !time(v.created_at) || !time(v.deadline) || !['next_attempt_at', 'lease_until', 'accepted_at'].every(k => v[k] === null || time(v[k]))) return null
  return v as unknown as NotificationDelivery
}
export function notificationPath(kind: NotificationKind, cursor = ''): string {
  if (!['decisions', 'deliveries'].includes(kind) || cursor.length > 4096) throw new Error('通知查询条件无效。')
  return '/admin/v1/notifications' + (kind === 'decisions' ? '/decisions' : '') + '?' + new URLSearchParams(cursor ? { cursor } : { page_size: '50' })
}
export function readNotificationPage(v: unknown, kind: NotificationKind): NotificationPage | null {
  if (!record(v) || !Array.isArray(v.items) || v.items.length > 50 || v.next_cursor !== null && (typeof v.next_cursor !== 'string' || !v.next_cursor || v.next_cursor.length > 4096)) return null
  const seen = new Set<string>()
  for (const item of v.items) { const row = kind === 'decisions' ? readDecision(item) : readDelivery(item); if (!row || seen.has(row.id)) return null; seen.add(row.id) }
  return v as unknown as NotificationPage
}
export function readDeliveryDetail(v: unknown, id: string): DeliveryDetail | null {
  if (!record(v) || !readDelivery(v.delivery) || (v.delivery as NotificationDelivery).id !== id || !Array.isArray(v.attempts) || v.attempts.length > 16) return null
  let last = 0
  for (const item of v.attempts) {
    if (!record(item) || !Number.isSafeInteger(item.number) || Number(item.number) <= last || Number(item.number) > (v.delivery as NotificationDelivery).attempts || !time(item.started_at) || item.finished_at !== null && !time(item.finished_at) || !word(item.outcome)) return null
    last = Number(item.number)
  }
  return v as unknown as DeliveryDetail
}