import type { CatalogAPI } from './catalog'
export interface RequestExample { id: string; title: string; explanation: string; body: Record<string, unknown>; snapshot?: boolean }
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
export function requestExamples(api: CatalogAPI): RequestExample[] {
  if (!object(api.request_example)) return []
  const base = { ...api.request_example }
  if (api.kind === 'source') {
    base.idempotency_key = '<NEW_IDEMPOTENCY_KEY>'
    return [
      { id: 'minimal', title: '受控测试作业 JSON 案例', explanation: '来自后端目录；幂等键已改为占位符。执行时应生成新键，同一次重试保留原键。本案例不是上游 GET Query。', body: base },
      { id: 'invalid', title: '已确认错误：超出测试分页上限', explanation: 'page_size=101 超出受控测试的 1..100 范围，后端拒绝该作业参数；不是提供方的分页上限。', body: { ...base, page_size: 101 } },
    ]
  }
  const fields = api.request?.properties ?? {}
  const examples: RequestExample[] = [{ id: 'minimal', title: '最小有效参数', explanation: '后端目录提供的最小格式有效案例；示例标识不保证对应部署中的真实实体，也不表示查询结果。', body: base }]
  const filter = 'is_active' in fields ? { is_active: false } : 'course_name' in fields ? { course_name: '<课程名称片段>' } : 'course_query' in fields ? { course_query: '<课程编号或名称片段>' } : 'name' in fields ? { name: '<姓名片段>' } : 'course_code' in fields ? { course_code: '<课程编号>' } : null
  examples.push({ id: 'filters', title: filter ? '常见筛选模板' : '完整定位条件', explanation: filter ? '在目录案例上添加已声明筛选字段；尖括号内容是待填写占位符，不是业务数据。不同筛选取交集。' : '此接口没有额外业务筛选；须完整保留教学班、计划课程、学年和学期定位条件。', body: { ...base, ...filter } })
  if ('page' in fields && 'page_size' in fields) {
    const pageSize = fields.page_size?.default
    if (typeof pageSize === 'number') {
      examples.push({ id: 'first', title: '分页：第一页', explanation: '每页条数取当前后端目录默认值。首次不带查询快照。', body: { ...base, page: 1, page_size: pageSize } })
      examples.push({ id: 'next', title: '分页：下一页', explanation: '复制命令前替换为同一接口、同一条件首响应返回的 X-Query-Snapshot；不能直接作为新查询第一页。', body: { ...base, page: 2, page_size: pageSize }, snapshot: true })
    }
    examples.push({ id: 'invalid', title: '已确认错误：无效页码', explanation: 'page=0 不符合业务接口正整数页码约束，返回 invalid_argument；该值只用于错误示例。', body: { ...base, page: 0 } })
  } else examples.push({ id: 'invalid', title: '已确认错误：无效学期', explanation: 'semester 只接受字符串 1 或 2；字符串 3 返回 invalid_argument。课表不分页。', body: { ...base, semester: '3' } })
  return examples
}
// POSIX shell quoting: copied commands never interpolate values or credentials from the session.
const quote = (value: string) => "'" + value.replaceAll("'", "'\"'\"'") + "'"
export function requestCommand(api: CatalogAPI, example: RequestExample): string {
  if (api.kind === 'source') return `curl --get ${quote('<SOURCE_ORIGIN>' + api.path)} --data-urlencode ${quote('access_token=<SOURCE_ACCESS_TOKEN>')} --data-urlencode ${quote('page=' + String(example.body.page ?? 1))} --data-urlencode ${quote('pageSize=' + String(example.body.page_size ?? 10))}`
  return `curl --request POST ${quote('<QUERY_ORIGIN>' + api.path)} --header ${quote('X-API-Key: <QUERY_API_KEY>')} --header ${quote('Content-Type: application/json')}${example.snapshot ? ' --header ' + quote('X-Query-Snapshot: <SAME_QUERY_SNAPSHOT>') : ''} --data-raw ${quote(JSON.stringify(example.body))}`
}
