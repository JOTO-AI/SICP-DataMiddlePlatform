import type { OperationNode } from './updateOperations'
export const operationKinds: Record<string, string> = {
  acquisition: '数据采集', builds: '物理副本构建', cleanup: '清理', progress: '进度提交', publication: '路由与发布', trace: '阶段诊断',
  route_shift: '查询路由转移', bundle_publish: '发布数据组合', business_check: '业务案例核验', confirm_progress: '确认同步进度', content_check: '内容校验', content_table: '表内容校验', drain: '等待旧副本排空', index_check: '索引校验', input_check: '输入校验', merge_table: '合并表数据', old_copy_check: '旧副本检查', prepare_target: '准备目标副本', publish: '提交发布', publish_confirm: '确认发布', sealing: '封存采集结果', target_check: '目标副本检查', usable_probe: '可用性探测', validate_copy: '副本校验', write_copy: '写入物理副本', write_table: '写入表数据',
}
export const operationStates: Record<string, string> = { staged: '已暂存，待确认', confirmed: '已确认', pending: '待处理', unknown: '未观测结论' }
export function operationMeaning(n: Pick<OperationNode, 'kind' | 'state' | 'evidence'>): string {
  if (n.evidence === 'platform_log' || n.evidence === 'log_diagnostics') return '这是阶段诊断观测。单次尝试结束不代表整个更新完成；恢复会话及重试保留各自记录，重叠阶段耗时不能相加作为任务耗时。'
  if (n.kind === 'route_shift') return n.state === 'confirmed' ? '已确认查询路由转移。此操作不等于发布新数据组合；后续构建及发布须分别确认。' : '查询路由转移尚未确认。不能据此认定新数据已生效。'
  if (n.kind === 'bundle_publish') return n.state === 'confirmed' ? '已确认数据组合发布。同步进度是否提交仍以任务的进度确认事实为准。' : '数据组合发布尚未确认，构建就绪或采集 100% 均不能代替发布确认。'
  if (n.kind === 'progress') return n.state === 'confirmed' ? '此任务的成功进度计划已确认。具体检查/发布结果仍看任务状态。' : n.state === 'staged' ? '成功进度计划已暂存，尚未确认提交。不能显示成新的已确认同步边界。' : '尚未观测到此节点的进度提交结论。'
  if (n.kind === 'cleanup') return '清理按该节点实际状态和时间展示；没有记录不代表已清理，清理状态也不代替数据发布结果。'
  if (n.kind === 'builds') return '各物理副本独立构建并校验。一个副本就绪不代表另一份已完成，也不代表数据已发布。'
  return '此节点只表示已返回的控制事实；未观测的阶段、数量和结束时间保持未知。'
}
export function updateMeaning(status: string, mode: string): string {
  switch (status) {
    case 'success_unchanged': return '本次检查完成且内容无变化，沿用已发布的数据组合；本次检查时间与旧组合发布时间分别保留。'
    case 'success_published': return mode === 'force_full' ? '本次强制全量重建并发布完成，即使内容未变化也不等同于普通无变化检查。' : '本次更新已成功发布；各副本及同步进度事实可在操作树中查看。'
    case 'published_pending_progress': return '新数据组合已发布，同步进度尚待确认；此时不标记整个更新成功完成。'
    case 'failed': return '本次更新失败。失败不表示既有数据组合已失效；当前可查询组合以运行总览的实际状态为准。'
    default: return '本次更新尚未确认终态。采集或构建完成不等于发布完成；现有组合是否可查询请查看运行总览。'
  }
}