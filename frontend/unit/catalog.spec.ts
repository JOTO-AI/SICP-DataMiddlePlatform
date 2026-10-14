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

import { sourceQueryFields } from '../src/app/sourceQueryFields'
test('provider Query metadata covers recorded sources without turning examples into defaults', () => {
  const sources = (backendCatalog() as CatalogAPI[]).filter(api => api.kind === 'source')
  expect(Object.keys(sourceQueryFields).sort()).toEqual(sources.map(api => api.id).sort())
  for (const api of sources) {
    const fields = sourceQueryFields[api.id]!
    expect(fields.find(field => field.name === 'pageSize')).toMatchObject({ type: 'Long', required: '是', defaultText: '未提供' })
    expect(fields.find(field => field.name === 'page_size')).toBeUndefined()
    expect(api.request.properties?.page_size?.default).toBe(10)
    expect(api.request.properties?.page_size?.limit).toBe('1..100')
  }
})

import { requestExamples, requestCommand } from '../src/app/requestExamples'
test('request examples preserve backend minimal payloads and isolate documentation placeholders', () => {
  for (const api of backendCatalog() as CatalogAPI[]) {
    const examples = requestExamples(api)
    const minimal = examples.find(item => item.id === 'minimal')!
    if (api.kind === 'business') {
      expect(minimal.body).toEqual(api.request_example)
      expect(examples.some(item => item.id === 'next')).toBe(api.snapshot)
      const next = examples.find(item => item.id === 'next')
      if (next) expect(requestCommand(api, next)).toContain('X-Query-Snapshot: <SAME_QUERY_SNAPSHOT>')
      expect(requestCommand(api, minimal)).toContain('X-API-Key: <QUERY_API_KEY>')
    } else {
      expect(minimal.body).toEqual({ ...(api.request_example as object), idempotency_key: '<NEW_IDEMPOTENCY_KEY>' })
      expect(requestCommand(api, minimal)).toContain('access_token=<SOURCE_ACCESS_TOKEN>')
      expect(requestCommand(api, minimal)).toContain('pageSize=10')
      expect(requestCommand(api, minimal)).not.toContain('idempotency_key')
    }
    expect(examples.find(item => item.id === 'invalid')).toBeDefined()
  }
  const api = (backendCatalog() as CatalogAPI[]).find(item => item.id === 'organizations.query')!
  expect(requestExamples(api).find(item => item.id === 'filters')?.body).toEqual({ is_active: false })
  // Shell-escaping boundary only, not a normal business record.
  const command = requestCommand(api, { id: 'quote', title: '', explanation: '', body: { name: "a'$(unsafe)" } })
  expect(command).toContain("'\"'\"'")
  expect(command).toContain('$(unsafe)')
})

import { apiCallsPath, readCallPage } from '../src/app/apiCalls'
import { managementCapture } from '../fixtures/backend'
test('API log queries use supported operation or explicit ID format and preserve existing event facts', () => {
  const apis = backendCatalog() as CatalogAPI[]
  const business = apis.find(api => api.id === 'students.query')!, source = apis.find(api => api.kind === 'source')!
  const path = apiCallsPath(business)
  expect(path).toContain('/platform/events?')
  expect(path).toContain('operation=students.query')
  const id = (managementCapture('source_test').response.data as { id: string }).id
  expect(apiCallsPath(source, id)).toContain(`test_run_id=${id}&id_format=raw`)
  expect(() => apiCallsPath(source)).toThrow()
  expect(apiCallsPath(source, '', 'raw', 'opaque-cursor')).toBe('/admin/v1/logs/upstream/events?cursor=opaque-cursor')
  const history = managementCapture('log_history').response.data as Record<string, any>
  const detail = managementCapture('log_detail').response.data as Record<string, any>
  // Existing empty history + protocol filter echo; no invented log/business rows.
  const page = { ...history, source: 'platform', filter: { ...history.filter, operation: business.id } }
  expect(readCallPage(page, business)?.items).toEqual([])
  expect(readCallPage(history, business)).toBeNull()
  expect(readCallPage({ ...page, items: [detail] }, business)?.items[0]).toMatchObject({ duration_ns: null, http_status: null, sequence: detail.sequence })
  expect(readCallPage({ ...page, next_cursor: 2 }, business)).toBeNull()
})
