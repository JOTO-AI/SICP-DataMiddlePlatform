import { test, expect } from '@playwright/test'
import { backendCapture, backendCatalog } from '../fixtures/backend'
import { gradeColumns, gradeQueryContext, gradeResponseFacts } from '../src/app/grades'
import { responseColumns, responseRecords, recordCell } from '../src/app/responseViews'
import type { CatalogAPI } from '../src/app/catalog'

test('both grade APIs share original record counts, XF credits, current affiliations and source update columns', () => {
  for (const id of ['students.grades.query', 'students.grades.batch-query']) {
    const capture = backendCapture(id), raw = JSON.stringify(capture.response), records = responseRecords(raw, 'business')
    const api = (backendCatalog() as CatalogAPI[]).find(api => api.id === id)!
    expect(responseColumns(api, records)).toBe(gradeColumns)
    const data = capture.response.data as { total: number; synced_at: string; items: { credits: number; source_updated_at: string; grade_id: string }[] }
    expect(gradeResponseFacts(raw)).toEqual({ total: String(data.total), syncedAt: data.synced_at })
    expect(gradeQueryContext(id, JSON.stringify(capture.request)).scope).toBe('跨学年 · 全部学期')
    expect(records.rows.map(row => recordCell(row, 'grade_id'))).toEqual(data.items.map(row => row.grade_id))
    for (const [index, row] of records.rows.entries()) {
      expect(recordCell(row, 'credits')).toBe(String(data.items[index]!.credits))
      expect(recordCell(row, 'source_updated_at')).toBe(data.items[index]!.source_updated_at)
    }
  }
  // Required scalar display boundaries; no additional grade/person rows are invented.
  expect(recordCell(new Map([['score', '0'], ['grade_point', 'null'], ['credits', '0']]), 'score')).toBe('0')
  expect(recordCell(new Map([['grade_point', 'null']]), 'grade_point')).toBe('null（未提供）')
  expect(recordCell(new Map([['credits', '0']]), 'credits')).toBe('0')
})

test('grade request scope follows backend latest, specified year, all and batch semantics without selecting rows locally', () => {
  const single = 'students.grades.query', base = backendCapture(single).request
  for (const [fields, phrase] of [ [{ academic_year: null }, '最近有成绩的学期'], [{ academic_year: null, semester: '1' }, '最近有第 1 学期成绩的学年'], [{ academic_year: '2026-2027' }, '2026-2027 · 全部学期'], [{ academic_year: '2026-2027', semester: '2' }, '2026-2027 · 第 2 学期'], [{ academic_year: 'all', semester: '1' }, '跨学年 · 第 1 学期'] ] as const) {
    expect(gradeQueryContext(single, JSON.stringify({ ...base, ...fields })).scope).toContain(phrase)
  }
  expect(gradeQueryContext('students.grades.batch-query', '{}').scope).toContain('不能解释为逐个学生')
  expect(gradeQueryContext(single).scope).toContain('不能推断')
  expect(gradeResponseFacts('{"data":{"total":null}}').total).toBe('未提供')
})
