import { actionReason } from './overview'
export type UpdateAction = 'incremental' | 'force_full' | 'table_force_full' | 'rerun'
export function readActionReasons(value: unknown): Record<string, string> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const actions = (value as { actions?: unknown }).actions
  if (!actions || typeof actions !== 'object' || Array.isArray(actions)) return null
  if (Object.entries(actions).some(([k, v]) => k.length > 64 || typeof v !== 'string' || !/^[a-z][a-z0-9_]{0,127}$/.test(v))) return null
  return actions as Record<string, string>
}
// Older captures/backends can omit individual advisory reasons. Concrete task
// preconditions still apply, and the mutation always rechecks under its lock.
export function actionAllows(reasons: Record<string, string> | null, action: UpdateAction): boolean {
  if (!reasons) return false
  const reason = reasons[action]
  return reason == null || reason === 'available_recheck_on_submit' || action === 'rerun' && reason === 'failed_task_selection_required'
}
export function actionExplanation(reasons: Record<string, string> | null, action: UpdateAction): string {
  if (!reasons) return '尚未取得操作条件，请刷新记录。'
  return reasons[action] ? actionReason(reasons[action]) : '后端未返回该操作的建议原因；按当前任务前置条件核对，提交时由后端再次检查。'
}