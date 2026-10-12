export type QueryKind = 'grades' | 'timetable' | 'organizations' | 'students' | 'staff' | 'classes' | 'members'
interface Field { key: string; label: string; required?: boolean; list?: boolean; semester?: boolean; pattern?: string; placeholder?: string }
interface Column { key: string; title: string }
export interface QueryOperation {
  operation: string; title: string; path: string; kind: QueryKind; paginated: boolean
  fields: Field[]; advanced: string[]; columns?: Column[]
}
const field = (key: string, label: string, required = false): Field => ({ key, label, required })
const term = (grade: boolean, required = true): Field[] => [
  { key: 'academic_year', label: '学年', required, pattern: grade ? '(all|[0-9]{4}-[0-9]{4})' : '[0-9]{4}-[0-9]{4}', placeholder: grade ? '如 2026-2027，或 all' : '如 2026-2027' },
  { key: 'semester', label: '学期', required: !grade, semester: true },
]
const gradeFilters = ['course_code', 'course_attribute_codes', 'course_nature_codes', 'course_type_codes', 'course_department_code', 'course_department_name', 'score_min', 'score_max']
const personColumns = (student: boolean): Column[] => [
  { key: student ? 'student_number' : 'staff_number', title: student ? '学号' : '工号' }, { key: 'name', title: '姓名' },
  { key: 'department.organization_code', title: '单位编号' }, { key: 'department.organization_name', title: '所属单位' },
  { key: 'status_code', title: '状态码' }, ...(student ? [{ key: 'class_name', title: '行政班' }, { key: 'major_name', title: '专业' }] : []),
  { key: 'email', title: '邮箱' }, { key: 'mobile', title: '电话' },
]
export const operations: QueryOperation[] = [
  { operation: 'students.grades.query', title: '学生成绩', path: '/api/v1/sicp/students/grades/query', kind: 'grades', paginated: true,
    fields: [field('student_number', '学号', true), ...term(true, false), field('course_name', '课程名称')], advanced: gradeFilters },
  { operation: 'students.grades.batch-query', title: '批量成绩', path: '/api/v1/sicp/students/grades/batch-query', kind: 'grades', paginated: true,
    fields: [{ key: 'student_numbers', label: '学号列表', list: true, placeholder: '多个学号以逗号或空格分隔' }, ...term(true), field('course_name', '课程名称')], advanced: [...gradeFilters, 'student_department_code', 'student_department_name', 'class_code', 'class_name'] },
  { operation: 'students.timetable.query', title: '学生课表', path: '/api/v1/sicp/students/timetable/query', kind: 'timetable', paginated: false,
    fields: [field('student_number', '学号', true), ...term(false), field('course_query', '课程检索')], advanced: [] },
  { operation: 'staff.timetable.query', title: '教师课表', path: '/api/v1/sicp/staff/timetable/query', kind: 'timetable', paginated: false,
    fields: [field('staff_number', '工号', true), ...term(false), field('course_query', '课程检索')], advanced: ['teaching_class_number'] },
  { operation: 'organizations.query', title: '组织查询', path: '/api/v1/sicp/organizations/query', kind: 'organizations', paginated: true,
    fields: [field('organization_code', '组织编号'), field('organization_name', '组织名称'), field('parent_organization_code', '上级组织编号')], advanced: ['category_codes', 'is_active'],
    columns: [{ key: 'organization_code', title: '组织编号' }, { key: 'organization_name', title: '组织名称' }, { key: 'parent_organization_code', title: '上级编号' }, { key: 'category_name', title: '组织类别' }, { key: 'is_active', title: '有效状态' }] },
  { operation: 'students.query', title: '学生信息', path: '/api/v1/sicp/students/query', kind: 'students', paginated: true,
    fields: [field('student_number', '学号'), field('name', '姓名'), field('department_code', '单位编号'), field('class_code', '行政班编号')],
    advanced: ['department_name', 'campus_code', 'major_code', 'grade', 'status_codes', 'enrollment_status_codes', 'on_campus_flag', 'student_category_codes', 'study_years', 'admission_year', 'expected_graduation_year', 'counselor_staff_number'], columns: personColumns(true) },
  { operation: 'staff.query', title: '教职工信息', path: '/api/v1/sicp/staff/query', kind: 'staff', paginated: true,
    fields: [field('staff_number', '工号'), field('name', '姓名'), field('department_code', '单位编号')], advanced: ['department_name', 'status_codes'], columns: personColumns(false) },
  { operation: 'staff.teaching-classes.query', title: '教师教学班', path: '/api/v1/sicp/staff/teaching-classes/query', kind: 'classes', paginated: true,
    fields: [field('staff_number', '工号', true), ...term(false), field('course_code', '课程编号')], advanced: [],
    columns: [{ key: 'teaching_class_number', title: '教学班编号' }, { key: 'teaching_class_name', title: '教学班名称' }, { key: 'planned_course_code', title: '计划课程编号' }, { key: 'course_name', title: '课程名称' }, { key: 'teachers', title: '授课教师' }, { key: 'student_count', title: '学生人数' }] },
  { operation: 'teaching-classes.students.query', title: '教学班学生', path: '/api/v1/sicp/teaching-classes/students/query', kind: 'members', paginated: true,
    fields: [field('teaching_class_number', '教学班编号', true), field('planned_course_code', '计划课程编号', true), ...term(false)], advanced: [],
    columns: [{ key: 'student_number', title: '学号' }, { key: 'name', title: '姓名' }, { key: 'department_code', title: '单位编号' }, { key: 'department_name', title: '所属单位' }, { key: 'class_code', title: '行政班编号' }, { key: 'class_name', title: '行政班名称' }] },
]
export const operationTitles = Object.fromEntries(operations.map(op => [op.operation, op.title]))

export function queryBody(op: QueryOperation, values: Record<string, string>, advanced: string, size: number): Record<string, unknown> {
  const extra = JSON.parse(advanced)
  if (!extra || Array.isArray(extra) || typeof extra !== 'object' || Object.keys(extra).some(key => !op.advanced.includes(key))) throw new Error('invalid filters')
  const body: Record<string, unknown> = { ...extra }
  for (const field of op.fields) {
    const value = values[field.key]?.trim()
    if (field.required && !value) throw new Error('missing field')
    if (value) body[field.key] = field.list ? value.split(/[\s,，]+/).filter(Boolean) : value
  }
  if (op.paginated) { body.page_size = size; body.page = 1 }
  return body
}

export function cell(row: Record<string, any>, key: string): string | number {
  const value = key.split('.').reduce<any>((item, part) => item?.[part], row)
  if (value == null || value === '') return '未提供'
  if (key === 'is_active') return value ? '有效' : '停用或未知'
  if (key === 'teachers') return value.map((teacher: { staff_number: string; name: string | null }) => teacher.name ? `${teacher.name}（${teacher.staff_number}）` : teacher.staff_number).join('、') || '未提供'
  return value
}
