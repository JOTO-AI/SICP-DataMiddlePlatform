import { objectTokens } from '../shared/jsonObject'
export const gradeColumns = [
  { key: 'grade_id', title: '成绩记录 ID' }, { key: 'student.student_number', title: '学号' }, { key: 'student.name', title: '姓名' },
  { key: 'academic_year', title: '学年' }, { key: 'semester', title: '学期' }, { key: 'course.course_code', title: '课程编号' }, { key: 'course.course_name', title: '当前课程名称' },
  { key: 'score', title: '成绩' }, { key: 'grade_point', title: '绩点' }, { key: 'credits', title: '成绩记录学分（XF）' },
  { key: 'student.department_code', title: '当前院系编号' }, { key: 'student.department_name', title: '当前院系名称' },
  { key: 'student.class_code', title: '当前行政班编号' }, { key: 'student.class_name', title: '当前行政班名称' },
  { key: 'course.department.organization_code', title: '当前开课单位编号' }, { key: 'course.department.organization_name', title: '当前开课单位名称' },
  { key: 'source_updated_at', title: '参与记录的源更新时间' },
]
export function gradeQueryContext(apiId: string, text?: string) {
  const fallback = { scope: '未保存本次请求条件，不能推断查询学期。' }
  try {
    if (!text) return fallback
    const body = JSON.parse(text)
    if (!body || typeof body !== 'object' || Array.isArray(body)) return fallback
    const year = body.academic_year, term = body.semester, single = apiId === 'students.grades.query'
    let scope: string
    if (year === 'all') scope = term ? `跨学年 · 第 ${term} 学期` : '跨学年 · 全部学期'
    else if (typeof year === 'string') scope = term ? `${year} · 第 ${term} 学期` : `${year} · 全部学期`
    else if (!single) scope = '批量成绩请求未提供学年；不能解释为逐个学生的最近学期。'
    else scope = term ? `最近有第 ${term} 学期成绩的学年 · 第 ${term} 学期` : '该学生最近有成绩的学期（先选学期，再筛选）'
    return { scope }
  } catch { return fallback }
}
export function gradeResponseFacts(raw: string): { total: string; syncedAt?: string | null } {
  try {
    const parsed = JSON.parse(raw), data = objectTokens(objectTokens(raw).get('data')!), total = data.get('total')
    return { total: total && /^(0|[1-9]\d*)$/.test(total) ? total : '未提供', syncedAt: typeof parsed.data.synced_at === 'string' || parsed.data.synced_at === null ? parsed.data.synced_at : undefined }
  } catch { return { total: '未提供' } }
}
