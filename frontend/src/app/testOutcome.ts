import type { ApiError } from '../api/http'
import type { BusinessTestResult } from './businessTest'
import type { SourceJob, SourceTestRead } from './sourceTest'
import { terminalSourceJob } from './sourceTest'
export interface TestOutcome { id: string; title: string; description: string; tone: 'neutral' | 'success' | 'warning' | 'danger' | 'progress' }
const state = (id: string, title: string, description: string, tone: TestOutcome['tone'] = 'neutral'): TestOutcome => ({ id, title, description, tone })
export function errorOutcome(error: ApiError): TestOutcome {
  if (['timeout', 'deadline_exceeded'].includes(error.code)) return state('timeout', '请求超时', '本次等待未取得完成确认，不自动重试；源作业请保留原编号或幂等键查询。', 'warning')
  if (error.code === 'network_error') return state('connection_failed', '连接失败', '未取得有效服务响应；源操作可能已被受理，不代表后台已取消。', 'danger')
  if (error.code === 'proxy_unavailable' || error.code === 'invalid_response' && [502, 503, 504].includes(error.status)) return state('proxy_failed', '代理响应异常', '网关或代理未提供有效接口响应，无法据此判断业务执行结果。', 'danger')
  if (error.code === 'invalid_catalog') return state('protocol_failed', '目录契约异常', '目录路径与已核对的查询契约不一致，本次未发送请求。请刷新目录或核对服务版本。', 'danger')
  if (error.code === 'invalid_response') return state('protocol_failed', '响应协议异常', '响应不能按当前接口契约解释；保留原文，不推断为成功或空结果。', 'danger')
  if (['unauthenticated', 'forbidden', 'session_changed'].includes(error.code)) return state('access_failed', '身份或权限异常', '请核对登录身份与权限，服务端仍负责最终授权。', 'danger')
  if (error.code === 'cancelled') return state('unconfirmed', '已停止等待', '当前操作结果尚未确认；停止等待不会撤销后台操作。', 'warning')
  return state('business_failed', '业务请求失败', '接口返回了失败信息，请按实际 code 和请求号核对原因；不会自动重试。', 'danger')
}
const idle = () => state('idle', '尚未测试', '案例只填入参数；执行后才会展示实际结果。')
const invalid = () => state('invalid', '参数校验失败', '草稿已保留，本次未发送请求，请修正标出的参数。', 'danger')
export function businessOutcome(input: { busy: boolean; validationError?: string; result?: BusinessTestResult | null }): TestOutcome {
  if (input.validationError) return invalid()
  if (input.busy) return state('waiting', '等待查询响应', '正在读取本地业务服务，不会自动重试。', 'progress')
  if (!input.result) return idle()
  if (input.result.error) return errorOutcome(input.result.error)
  const data = input.result.response?.body.data
  if (!data || typeof data !== 'object' || !('items' in data) || !Array.isArray(data.items)) return state('protocol_failed', '响应协议异常', '响应缺少合法 items 数组，不能当作空结果。原文已保留。', 'danger')
  return data.items.length ? state('success', '查询成功', `本次响应包含 ${data.items.length} 条记录。`, 'success') : state('empty', '本次结果为空', '本次响应 items 为空；不据此将服务端 total 改为 0，也不表示全部数据为空。')
}
export function sourceOutcome(input: { busy: boolean; validationError?: string; pending: boolean; job?: SourceJob; response?: SourceTestRead; error?: ApiError; waitingStopped: boolean }): TestOutcome {
  if (input.validationError) return invalid()
  if (input.busy) return state('waiting', input.job?.status === 'queued' ? '作业排队中' : input.job?.status === 'running' ? '作业执行中' : '等待源测试回执', '本轮只等待原作业的单页结果；尚无 result 不等于失败。', 'progress')
  if (input.error) return errorOutcome(input.error)
  const job = input.job
  if (!job) return input.pending ? state('unconfirmed', '作业状态待确认', '恢复了原请求；尚未自动提交或查询，请使用原键重试受理或查询原作业。', 'warning') : idle()
  // Job lifecycle is authoritative. A missing cache entry while queued/running is not terminal failure.
  if (!terminalSourceJob(job)) return state('unconfirmed', '作业状态待确认', input.waitingStopped ? '本轮等待已达上限，请继续查询原作业；不自动提交新测试。' : '作业仍在排队或运行，尚未取得终态。', 'warning')
  if (job.status !== 'succeeded') {
    if (job.summary.failure === 'request_timeout') return state('timeout', '源请求超时', '后端报告本次源请求超时；不能推断上游未处理，也不会自动重测。', 'warning')
    if (job.summary.failure === 'business') return state('business_failed', '上游业务失败', '后端记录了上游业务错误，请查看实际作业摘要中的数值 code。', 'danger')
    return state('source_failed', job.status === 'interrupted' ? '源作业执行中断' : '源作业失败', '作业终态以服务端记录为准；管理查询 HTTP 200 不代表源测试成功。', 'danger')
  }
  const data = input.response?.body.data
  if (!data || data.result_state !== 'available') return state('unavailable', '结果不可获取', '作业成功，但当前未取得结果。可查询原作业；不会自动再次测试。', 'warning')
  const result = data.result as { list: unknown[] }
  return result.list.length ? state('success', '源测试成功', `本页取得 ${result.list.length} 条记录；只测试此页，不代表全部源数据已校验。`, 'success') : state('empty', '本页结果为空', '作业成功且本页 list 为空；保留实际 total，不自动补页。')
}
