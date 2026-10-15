import { updateDetailPath } from './updateEvents'
export interface OperationNode {
  id: string; parent_operation_id: string | null; kind: string; state: string; evidence: string
  instance: string | null; slot: number | null; generation: string | null; count: string | null
  started_at: string | null; finished_at: string | null
  build?: { id: string; session_id: string; instance: string; owner_generation: string; attempt: number; state: string; scope: Record<string, unknown>; duration_ns: string | null; cumulative_ns: string | null; [key: string]: unknown }
}
export interface OperationPage { items: OperationNode[]; next_cursor: string | null; parent_operation_id: string }
const text = (v: unknown, max = 512): v is string => typeof v === 'string' && v.length > 0 && v.length <= max && !/[\u0000-\u001f\u007f]/.test(v)
const decimal = (v: unknown) => v === null || typeof v === 'string' && /^\d{1,19}$/.test(v)
const at = (v: unknown) => v === null || typeof v === 'string' && v.length <= 64 && Number.isFinite(Date.parse(v))
export const operationTitles: Record<string, string> = { acquisition: '数据采集', builds: '物理副本构建', cleanup: '清理', progress: '进度提交', publication: '路由与发布', trace: '阶段诊断' }
export function expandableOperation(node: OperationNode) {
  return node.parent_operation_id === null && Object.hasOwn(operationTitles, node.id) || node.parent_operation_id === 'trace' && /^trace:[a-z_]+$/.test(node.id)
}
export function operationPath(task: string, parent = '', cursor = '') {
  if (parent && !text(parent, 128) || cursor && !text(cursor, 4096)) throw new Error('操作树查询标识无效。')
  const query = new URLSearchParams(cursor ? { cursor } : { page_size: '50' })
  if (parent) query.set('parent_operation_id', parent)
  return updateDetailPath(task) + '/operations?' + query
}
export function readOperationPage(value: unknown, parent: string): OperationPage | null {
  if (!value || typeof value !== 'object') return null
  const p = value as OperationPage
  if (p.parent_operation_id !== parent || !Array.isArray(p.items) || p.items.length > 50 || p.next_cursor !== null && !text(p.next_cursor, 4096)) return null
  const ids = new Set<string>()
  for (const n of p.items) {
    if (!n || !text(n.id) || ids.has(n.id) || n.parent_operation_id !== (parent || null) || !text(n.kind, 128) || !text(n.state, 128) || !text(n.evidence, 128) || n.instance !== null && !text(n.instance, 128) || n.slot !== null && (!Number.isInteger(n.slot) || n.slot < 0 || n.slot > 32767) || !decimal(n.generation) || !decimal(n.count) || !at(n.started_at) || !at(n.finished_at)) return null
    const b = n.build
    if (b && (b.id !== n.id || !text(b.session_id, 128) || !text(b.instance, 128) || !decimal(b.owner_generation) || typeof b.owner_generation !== 'string' || !Number.isSafeInteger(b.attempt) || b.attempt < 1 || b.state !== n.state || !b.scope || typeof b.scope !== 'object' || Array.isArray(b.scope) || !decimal(b.duration_ns) || !decimal(b.cumulative_ns))) return null
    ids.add(n.id)
  }
  return p
}