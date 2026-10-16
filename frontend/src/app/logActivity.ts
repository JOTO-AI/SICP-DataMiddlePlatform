import { validLogContext, type LogContext } from './logContext'
import type { LogEvent } from './logHistory'
import { logKinds, logOutcomes, logPhases } from './logDetail'
import { operationKinds } from './updateMeaning'
import { operations } from '../queryOperations'

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
// Older detail responses still carry build/source metadata; list rows never
// borrow context from another event, even when their task IDs are identical.
function context(event: LogEvent): LogContext {
  if (event.context) return event.context
  const metadata = (event as LogEvent & { metadata?: unknown }).metadata
  if (!object(metadata)) return {}
  const build = metadata.build
  const value = object(build) ? { stage: build.stage, table: build.table, attempt: build.attempt, operation: build.operation } :
    { table: metadata.source, page: metadata.page, mode: metadata.mode }
  return validLogContext(value) ? value : {}
}
const name = (dictionary: Record<string, string>, key: string) => Object.hasOwn(dictionary, key) ? dictionary[key]! : key
const operationName = (key: string) => operations.find(item => item.operation === key)?.title ?? key
export function logActivity(event: LogEvent) {
  const c = context(event)
  const operation = c.operation || event.operation
  const subject = c.stage ? name(operationKinds, c.stage) :
    operation ? operationName(operation) :
    c.table ? '采集表数据' :
    event.audit?.action ? name({ source_test: '测试源接口', notification_settings: '修改通知设置', notification_test: '测试通知', retention_settings: '修改保留策略', cleanup: '清理日志' }, event.audit.action) :
    event.kind === 'step' ? '更新步骤（名称未记录）' : logKinds[event.kind]
  const details: string[] = []
  if (c.table) details.push('数据表：' + c.table)
  if (c.instance) details.push('副本：' + c.instance)
  if (c.slot !== undefined) details.push('槽位：' + c.slot)
  if (c.stage && operation) details.push('接口：' + operationName(operation))
  if (c.page) details.push('第 ' + c.page + ' 页')
  if (c.mode) details.push(name({ full: '全量采集', force_full: '强制全量采集', incremental: '增量采集' }, c.mode))
  if (c.attempt) details.push('第 ' + c.attempt + ' 次执行')
  if (event.attempt_no) details.push('第 ' + event.attempt_no + ' 次调用尝试')
  return { subject, title: subject + ' · ' + logPhases[event.phase], details: details.join(' · '),
    result: event.outcome ? logOutcomes[event.outcome] : event.phase === 'started' ? '已记录开始' : '结果未记录' }
}
