import type { CatalogAPI } from './catalog'
import { catalogName } from './catalog'

// Translate only reviewed backend metadata; new/changed text remains visible verbatim.
const purposes: Record<string, string> = {
  'organizations.query': '查询组织信息，支持按名称、上级组织和有效状态筛选。',
  'students.query': '查询学生信息，支持学籍、单位、专业和年级等条件筛选。',
  'staff.query': '查询教职工信息，支持工号、姓名、单位和状态筛选。',
  'students.grades.query': '查询指定学生的成绩，支持课程、学期和成绩范围筛选。',
  'students.grades.batch-query': '按学年批量查询成绩，可结合学号、课程和成绩范围筛选。',
  'students.timetable.query': '查询指定学生在某学年、学期的课表。',
  'staff.timetable.query': '查询指定教师在某学年、学期的课表，可按教学班筛选。',
  'staff.teaching-classes.query': '查询教师在指定学年、学期的教学班及实际学生人数。',
  'teaching-classes.students.query': '按教学班、计划课程、学年和学期查询学生名单。',
}
const notes: Record<string, string> = {
  'Catalog source fields describe each record.': '响应字段描述上游返回的单条记录。',
  'Test credentials and destination are fixed by deployment; at most one page, 2 MiB and 30 seconds including queue time.': '测试凭据和目标地址由服务端配置。每次最多读取一页，结果上限 2 MiB，最长 30 秒（含排队）。',
  'Test requests never advance synchronization progress; raw results expire within ten minutes.': '接口测试不推进同步进度；原始测试结果在十分钟内过期。',
  'Business execution uses the existing /console-api/query proxy.': '业务查询通过同源代理 /console-api/query 执行。',
  'Unknown fields are rejected; null optional request fields are treated as omitted.': '不接受未声明的字段；可选参数为 null 时按未填写处理。',
  'Response timestamps follow the existing business contract and are not log timestamps.': '响应中的时间遵循业务接口定义，与日志记录时间分别解释。',
  "Use each operation's own X-Query-Snapshot for subsequent pages. For staff teaching-classes and class students, compare X-SICP-Combination on every page and reconcile student_count with the complete unique roster; discard and restart the round if versions or counts differ. Never send one operation's token to another.": '翻页须使用当前接口自己的 X-Query-Snapshot。教师教学班与教学班学生接口须逐页核对 X-SICP-Combination，并用完整去重名单核对 student_count；版本或人数不一致时重新查询，不跨接口复用快照。',
  'Timetable is not paginated and does not accept X-Query-Snapshot.': '课表接口不分页，也不接受 X-Query-Snapshot。',
}
const limits: Record<string, string> = {
  'required nonblank string; identifiers preserved': '必填非空字符串；标识符按原值保留',
  'missing and null are distinct; source text preserved': '字段缺失与 null 含义不同；保留源文本',
  'any JSON token retained without numeric conversion': '保留原始 JSON 值，不转换数字',
  'YYYY-MM-DD[T or space]HH:MM:SS with up to 6 fractional digits; no timezone': '格式 YYYY-MM-DD HH:MM:SS（日期与时间也可用 T 分隔）；小数秒最多 6 位，不含时区',
  '1..1000 nonblank values when supplied': '填写时须包含 1–1000 个非空值',
  'nonblank; <=512 UTF-8 bytes; no control characters or U+FFFD': '非空，最多 512 个 UTF-8 字节；不含控制字符或替换字符 U+FFFD',
  'YYYY-YYYY; consecutive years; first year 1000..9998': '格式 YYYY-YYYY，年份相邻；起始年份为 1000–9998',
  'YYYY-YYYY; consecutive years; first year 1000..9998; or all': '格式 YYYY-YYYY，年份相邻；起始年份为 1000–9998；all 表示全部学年',
  '1 or 2': '可选值：1 或 2',
  'finite number; score_min <= score_max': '有限数值；最低成绩不能大于最高成绩',
  '0001..9999; four digits': '四位年份字符串，范围 0001–9999',
}
export function catalogPurpose(api: CatalogAPI): string {
  if (api.purpose === api.id && purposes[api.id]) return purposes[api.id]!
  if (api.kind === 'source' && api.purpose === `Read one page of ${api.id}`) return `从上游读取一页${catalogName(api)}数据。`
  return api.purpose
}
export const catalogNote = (text: string): string => notes[text] ?? text
export function catalogLimit(text: unknown): string {
  if (typeof text !== 'string' || !text) return '未声明'
  if (limits[text]) return limits[text]!
  const range = /^(\d+)\.\.(\d+)$/.exec(text)
  return range ? `范围：${range[1]}–${range[2]}` : text
}
