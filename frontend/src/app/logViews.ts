import { emptyLogFilters, type LogFilters } from './logHistory'
export const logViews = [
  { id: 'requests', title: '接口调用', source: 'platform', kind: 'request' },
  { id: 'updates', title: '更新操作', source: 'platform', kind: 'task' },
  { id: 'upstream', title: '原数据接口', source: 'upstream', kind: '' },
  { id: 'audit', title: '管理审计', source: 'platform', kind: 'audit' },
] as const
export function logViewFilters(id: string): LogFilters | null {
  const view = logViews.find(v => v.id === id)
  return view ? { ...emptyLogFilters(view.source), kind: view.kind } : null
}
export function selectedLogView(f: LogFilters): string {
  if (f.source === 'upstream') return 'upstream'
  if (f.kind === 'task' || f.kind === 'step') return 'updates'
  return logViews.find(v => v.source === f.source && v.kind === f.kind)?.id ?? ''
}