import type { CatalogAPI } from './catalog'
import { catalogLimit } from './catalogCopy'
import { sourceFieldLabels } from './sourceFieldLabels'

// Documentation metadata only. Field constraints always come from the backend.
const labels: Record<string, string> = {
  academic_year: '学年', admission_month: '入学月份', admission_year: '入学年份', attribute_code: '课程属性码', attribute_name: '课程属性名称',
  campus_code: '校区编号', campus_name: '校区名称', category_code: '类别码', category_codes: '类别码集合', category_name: '类别名称',
  class_code: '行政班编号', class_name: '行政班名称', counselor: '辅导员', counselor_staff_number: '辅导员工号', course: '课程信息',
  course_attribute_codes: '课程属性码集合', course_code: '课程编号', course_department_code: '开课单位编号', course_department_name: '开课单位名称',
  course_name: '课程名称', course_nature_codes: '课程性质码集合', course_query: '课程编号或名称检索', course_type_codes: '课程类型码集合',
  credits: '学分', dates: '上课日期集合', department: '所属单位信息', department_code: '所属单位编号', department_name: '所属单位名称',
  email: '电子邮箱', enrolled_count: '源课表选读人数', enrollment_status_code: '学籍状态码', enrollment_status_codes: '学籍状态码集合',
  expected_graduation_date: '预计毕业日期', expected_graduation_year: '预计毕业年份', grade: '年级', grade_id: '成绩记录标识', grade_point: '绩点',
  is_active: '是否有效', items: '结果记录集合', location: '上课地点', major_code: '专业编号', major_name: '专业名称', mobile: '移动电话',
  name: '姓名', nature_code: '课程性质码', nature_name: '课程性质名称', on_campus_flag: '在校标识原码', organization_code: '组织编号',
  organization_name: '组织名称', page: '页码', page_size: '每页条数', parent_organization_code: '直接上级组织编号', planned_course_code: '计划课程编号',
  retake_course_code: '重修课程编号', retake_course_name: '重修课程名称', retake_flag: '重修标识原码', room_code: '教室编号',
  score: '成绩；可能为数值、文本或空值', score_max: '成绩上界（含）', score_min: '成绩下界（含）', sections: '节次集合', semester: '学期',
  source_updated_at: '参与该记录的源数据最大 ETL 时间', staff_number: '工号', status_code: '状态码', status_codes: '状态码集合',
  student: '学生信息', student_category_code: '学生类别码', student_category_codes: '学生类别码集合', student_category_name: '学生类别名称',
  student_count: '教学班实际去重学生人数', student_department_code: '学生当前所属单位编号', student_department_name: '学生当前所属单位名称',
  student_number: '学号', student_numbers: '学号集合', study_years: '学制年数', synced_at: '所需源表成功检查确认时间的最小值',
  teacher_names: '教师姓名集合', teacher_staff_numbers: '教师工号集合', teachers: '教师信息集合', teaching_class_name: '教学班名称',
  teaching_class_number: '教学班编号', total: '符合条件的记录总数', total_hours: '总学时', type_code: '课程类型码', type_name: '课程类型名称',
  weekday: '星期', weeks: '教学周集合',
}
export interface SchemaRow { path: string; type: string; required: string; nullable: string; defaultText: string; limit: string; description: string }
const flag = (value: unknown) => value === true ? '是' : value === false ? '否' : '未声明'
export function fieldDescription(api: CatalogAPI, name: string): string {
  if (api.kind === 'source') return sourceFieldLabels[api.id]?.[name] ?? labels[name] ?? '后端目录未提供中文语义'
  if (name === 'credits' && api.id.includes('grades.')) return '成绩记录学分（XF），不替换为当前课程学分'
  return labels[name] ?? '后端目录未提供中文语义'
}
export function schemaRows(api: CatalogAPI, schema: unknown): { rows: SchemaRow[]; incomplete: boolean } {
  const rows: SchemaRow[] = []
  let incomplete = false
  function visit(value: unknown, path: string, name: string, depth: number) {
    if (rows.length >= 1000 || depth > 12) { incomplete = true; return }
    if (!value || typeof value !== 'object' || Array.isArray(value)) { incomplete = true; return }
    const field = value as Record<string, unknown>
    if (!(path === '$' && field.type === 'object' && field.properties && typeof field.properties === 'object' && !Array.isArray(field.properties))) rows.push({ path, type: typeof field.type === 'string' ? field.type : '未声明', required: flag(field.required), nullable: flag(field.nullable),
      defaultText: field.default == null ? '未声明' : JSON.stringify(field.default), limit: catalogLimit(field.limit),
      description: path === '$' ? '根结构' : fieldDescription(api, name) })
    if (field.properties != null) {
      if (typeof field.properties !== 'object' || Array.isArray(field.properties)) incomplete = true
      else for (const [key, child] of Object.entries(field.properties).sort(([, a], [, b]) => Number(!!b && typeof b === 'object' && (b as Record<string, unknown>).required === true) - Number(!!a && typeof a === 'object' && (a as Record<string, unknown>).required === true))) visit(child, path === '$' ? key : `${path}.${key}`, key, depth + 1)
    }
    if (field.items != null) visit(field.items, `${path}[]`, name, depth + 1)
  }
  visit(schema, '$', '', 0)
  return { rows, incomplete }
}
const timetableRules = ['须指定身份、学年和学期；学年不接受 all。课表不分页，也不使用查询快照。', '教师工号与姓名数组分别保留，不按下标配对；日期、周次、节次按实际记录显示，不仅按课程编号合并。选读人数来自源课表，不能替代教学班去重学生人数。']
const gradeRules = ['不同筛选条件取交集，码值数组内部取并集；成绩范围包含上下界，不限定为 0-100，文本成绩和空成绩不参与数值范围比较。', '总数按成绩记录计，不按学生或课程去重。学分来自成绩记录 XF；课程属性取当前课程，学生单位/行政班取当前学籍；关联缺失仍保留成绩记录。']
export const operationRules: Record<string, string[]> = {
  'students.grades.query': ['未指定学年/学期时，先选择该学生有成绩的最新学期，再应用课程等筛选；筛选无结果不会回退到旧学期。只指定学年查询该年全部学期；只指定学期选择有该学期成绩的最新学年；all 跨学年但仍应用学期筛选。', ...gradeRules],
  'students.grades.batch-query': ['学年必填，可为 all；不会按每个学生分别选择最新学期。学号集合可省略。', ...gradeRules],
  'students.timetable.query': ['按学号读取实际课表。', ...timetableRules],
  'staff.timetable.query': ['按工号读取实际课表，可进一步指定教学班编号。', ...timetableRules],
  'organizations.query': ['上级组织编号只筛选直接子级，不递归查询后代；没有上级信息不代表已取得完整组织树。', 'is_active=false 是有效筛选；不指定状态时不默认只查有效组织。顶层组织有效状态为布尔值，人员嵌套的单位状态可为空。'],
  'students.query': ['可不提供筛选条件；省略状态筛选时保留全部状态。不同条件取交集，状态码集合内部取并集。', '入学/预计毕业年份为四位年份字符串，学制为正整数；原码不转换为前端推测的名称。单位和行政班来自当前学籍。'],
  'staff.query': ['可不提供筛选条件；省略状态筛选时保留全部状态。不同条件取交集，状态码集合内部取并集。', '工号保留原字符串；单位信息缺失可为空，不补造组织。'],
  'staff.teaching-classes.query': ['工号、学年和学期必填，学年不接受 all。student_count 是实际教学班学生关联的去重人数，不是源课表选读人数；读取失败不能当作零人。', '进入教学班学生接口时须携带教学班编号、计划课程编号、学年和学期，重新取得该接口自己的查询快照。'],
  'teaching-classes.students.query': ['教学班编号、计划课程编号、学年和学期共同定位教学班，均必填，学年不接受 all。', '逐页核对 X-SICP-Combination；不同接口不复用查询快照。全部页面去重后方可与教学班人数核对，单页或中断的名单不是完整名单。'],
}
export const commonBusinessRules = [
  'Body 为单个 UTF-8 JSON 对象；字段名区分大小写，未知字段、重复字段、尾随内容会被拒绝。正文最多 65536 字节。可选 null 等同省略。',
  '标识符保留原字符串，不去前后空格或转数字。名称使用区分大小写的字面包含匹配，%、_、反斜线不是通配符。数组提供时不能为空，最多 1000 个非空字符串。',
  '页码/每页条数接受正整数 JSON 数字，不接受字符串、小数或指数形式。分页快照只绑定当前接口与筛选条件，除 page 外参数不能改变；默认有效期 5 分钟（服务端可配置），翻页不续期。',
  'synced_at 表示当前组合所需源表的成功检查确认时间，不是请求时间；source_updated_at 来自参与记录的源 ETL 时间。',
]

/** Keep primitive array elements in the parameter's own row, not as fake required parameters. */
export function parameterSchemaRows(api: CatalogAPI) {
  const result = schemaRows(api, api.request)
  const byPath = new Map(result.rows.map(row => [row.path, row]))
  const folded = new Set<string>()
  const rows = result.rows.map(row => {
    const element = byPath.get(row.path + '[]')
    if (row.type !== 'array' || !element || !['string', 'number', 'integer', 'boolean', 'json'].includes(element.type)) return row
    folded.add(element.path)
    return { ...row, type: element.type + '[]', limit: row.limit + '；元素：' + element.limit + (element.nullable === '否' ? '，不允许 null' : element.nullable === '是' ? '，允许 null' : '，可空性未声明') }
  }).filter(row => !folded.has(row.path))
  return { ...result, rows }
}
