import { test, expect } from '@playwright/test'
import { backendCatalog } from '../fixtures/backend'
import { readCatalogAPI, readCatalog, filterCatalog, type CatalogAPI } from '../src/app/catalog'

test('catalog validates the generated 9+14 entries and filters metadata only', () => {
  const items = backendCatalog() as CatalogAPI[]
  const catalog = readCatalog({ version: items[0]!.version, items })!
  expect(catalog.items).toHaveLength(23)
  expect(filterCatalog(catalog.items, '', 'business')).toHaveLength(9)
  expect(filterCatalog(catalog.items, '', 'source')).toHaveLength(14)
  expect(filterCatalog(catalog.items, '学生成绩', '')[0]?.id).toBe('students.grades.query')
  expect(filterCatalog(catalog.items, '上游接口', '')).toHaveLength(14)
  expect(filterCatalog(catalog.items, '/students/grades/query', '')[0]?.id).toBe('students.grades.query')
  expect(readCatalog({ version: 'mismatched-version', items })).toBeNull()
  expect(readCatalog({ version: items[0]!.version, items: [items[0], items[0]] })).toBeNull()
  expect(readCatalog(null)).toBeNull()
})

test('API detail binds to the requested backend identity', () => {
  const item = (backendCatalog() as CatalogAPI[]).find(api => api.id === 'students.query')!
  expect(readCatalogAPI(item, item.id)).toEqual(item)
  expect(readCatalogAPI(item, 'staff.query')).toBeNull()
  expect(readCatalogAPI(null, item.id)).toBeNull()
})
import { schemaRows, operationRules } from '../src/app/catalogSchema'

test('custom Field adapter preserves all generated schemas and documentation coverage', () => {
  for (const api of backendCatalog() as CatalogAPI[]) {
    for (const field of [api.request, api.response]) {
      const result = schemaRows(api, field)
      expect(result.incomplete, api.id).toBe(false)
      expect(result.rows.length).toBeGreaterThan(1)
      expect(result.rows.filter(row => row.description === '后端目录未提供中文语义'), api.id).toEqual([])
    }
    if (api.kind === 'business') expect(operationRules[api.id]?.length).toBeGreaterThan(0)
  }
  const apis = backendCatalog() as CatalogAPI[]
  const student = apis.find(api => api.id === 'students.query')!
  const grade = apis.find(api => api.id === 'students.grades.query')!
  expect(schemaRows(student, student.request).rows.find(row => row.path === 'page_size')?.defaultText).toBe('1000')
  expect(schemaRows(grade, grade.request).rows.find(row => row.path === 'page_size')?.defaultText).toBe('100')
  const nested = schemaRows(student, student.response).rows.find(row => row.path === 'items[].department.is_active')!
  expect(nested).toMatchObject({ type: 'boolean', required: '是', nullable: '是', defaultText: '未声明' })
  const org = apis.find(api => api.id === 'organizations.query')!
  expect(schemaRows(org, org.response).rows.find(row => row.path === 'items[].is_active')).toMatchObject({ required: '是', nullable: '否' })
  expect(schemaRows(grade, grade.response).rows.find(row => row.path === 'items[].credits')?.description).toContain('XF')
})

test('malformed or oversized Field metadata is visibly incomplete', () => {
  const api = (backendCatalog() as CatalogAPI[])[0]!
  expect(schemaRows(api, null).incomplete).toBe(true)
  expect(schemaRows(api, { properties: [] }).incomplete).toBe(true)
  const cycle: Record<string, unknown> = { type: 'array', default: false }
  cycle.items = cycle
  const result = schemaRows(api, cycle)
  expect(result.incomplete).toBe(true)
  expect(result.rows).toHaveLength(13)
  expect(result.rows[0]).toMatchObject({ required: '未声明', nullable: '未声明', defaultText: 'false' })
})
