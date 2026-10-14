export interface UpdateEvent {
  id: string; task_id: string; mode: string; status: string; predecessor_id: string | null
  accepted_at: string; started_at: string | null; ready_at: string | null; published_at: string | null; checked_at: string | null; finished_at: string | null
}
export interface UpdatePage { items: UpdateEvent[]; next_cursor: string | null }
export const updateModes: Record<string, string> = { initialize: '初始化', incremental: '增量同步', force_full: '强制全量' }
export const updateTimes = [
  ['accepted_at', '受理时间'], ['started_at', '实际开始'], ['ready_at', '构建就绪'], ['published_at', '发布完成'], ['checked_at', '检查完成'], ['finished_at', '任务结束'],
] as const
export const validUpdateID = (value: unknown): value is string => typeof value === 'string' && /^[A-Za-z0-9_:-]{1,128}$/.test(value)
const word = (value: unknown) => typeof value === 'string' && value.length > 0 && value.length <= 128
function taskFields(value: Record<string, unknown>): boolean {
  return validUpdateID(value.task_id) && word(value.mode) && word(value.status) &&
    typeof value.accepted_at === 'string' && Number.isFinite(Date.parse(value.accepted_at)) &&
    (value.predecessor_id == null || validUpdateID(value.predecessor_id)) &&
    updateTimes.every(([key]) => value[key] == null || typeof value[key] === 'string' && value[key].length <= 64 && Number.isFinite(Date.parse(value[key])))
}
export function updateListPath(cursor = ''): string {
  if (cursor.length > 4096) throw new Error('更新事件游标超过协议上限，请重新读取首批。')
  return '/admin/v1/update-events?' + new URLSearchParams(cursor ? { cursor } : { page_size: '50' })
}
export function updateDetailPath(id: string): string {
  if (!validUpdateID(id)) throw new Error('更新事件编号格式无效。')
  return '/admin/v1/update-events/' + encodeURIComponent(id)
}
export function readUpdatePage(value: unknown): UpdatePage | null {
  if (!value || typeof value !== 'object') return null
  const page = value as UpdatePage
  if (!Array.isArray(page.items) || page.items.length > 50 || page.next_cursor !== null && (typeof page.next_cursor !== 'string' || !page.next_cursor || page.next_cursor.length > 4096)) return null
  const ids = new Set<string>()
  for (const item of page.items) {
    // Current backend maps Event.ID and TaskID to the same task; reject conflicting identities.
    if (!item || typeof item !== 'object' || !validUpdateID(item.id) || item.task_id !== item.id || ids.has(item.id) || !taskFields(item as unknown as Record<string, unknown>)) return null
    ids.add(item.id)
  }
  return page
}
export interface UpdateDetail extends Omit<UpdateEvent, 'id'> { operator?: string | null; progress_confirmed?: boolean | null; failure?: string | null; diagnostics_unavailable?: boolean }
export function readUpdateDetail(value: unknown, id: string): UpdateDetail | null {
  if (!value || typeof value !== 'object' || !taskFields(value as Record<string, unknown>) || (value as UpdateDetail).task_id !== id) return null
  const detail = value as UpdateDetail
  if (detail.operator != null && typeof detail.operator !== 'string' || detail.failure != null && typeof detail.failure !== 'string' ||
    detail.progress_confirmed != null && typeof detail.progress_confirmed !== 'boolean' || detail.diagnostics_unavailable != null && typeof detail.diagnostics_unavailable !== 'boolean') return null
  return detail
}