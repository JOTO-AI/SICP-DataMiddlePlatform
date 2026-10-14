import { test, expect } from '@playwright/test'
import { backendCapture, backendCatalog } from '../fixtures/backend'
import type { CatalogAPI } from '../src/app/catalog'
import { validateTestRequest, testParameters, editableExample } from '../src/app/testRequest'
import { parseRequestObject } from '../src/shared/json'
const catalog = backendCatalog() as CatalogAPI[]
const api = (id: string) => catalog.find(item => item.id === id)!

test('all generated backend success requests pass without changing identifiers or values', () => {
  for (const item of catalog.filter(item => item.kind === 'business')) {
    const captured = backendCapture(item.id)
    expect(validateTestRequest(item, JSON.stringify(captured.request))).toEqual(captured.request)
    expect(() => validateTestRequest(item, JSON.stringify(backendCapture(item.id, 'invalid_argument').request))).toThrow()
    expect(validateTestRequest(item, JSON.stringify(item.request_example))).toEqual(item.request_example)
  }
  const captured = backendCapture('students.grades.query')
  const original = JSON.stringify(captured.request, null, 4) + '\n'
  expect(validateTestRequest(api(captured.operation), original).student_number).toBe(captured.request.student_number)
  expect(original).toContain('00202600001')
})

test('query parser boundaries match the reviewed backend rules and keep JSON tokens meaningful', () => {
  const students = api('students.query'), grade = api('students.grades.query')
  const base = backendCapture(grade.id).request
  for (const body of ['{"page":1,"page":2}', '{"page":1,"pa\\u0067e":2}', '{"page":1.0}', '{"page":1e2}', '{"page":"1"}', '{"page":0}', '{"page_size":1001}', '{"name":""}', '{"name":"\\u0085"}', '{"name":"\\ud800"}', '{"name":"\\ufffd"}', '{"is_active":true}', '{"admission_year":"0000"}', '{"study_years":2147483648}', '{"status_codes":[]}', '{"status_codes":[null]}']) {
    expect(() => validateTestRequest(students, body), body).toThrow()
  }
  expect(() => parseRequestObject('{"a":{"b":[1,"}"]},"a":null}')).toThrow(/重复/)
  expect(parseRequestObject('{"a":{"b":[1,"}"]},"c":"a,b:c"}')).toEqual({ a: { b: [1, '}'] }, c: 'a,b:c' })
  expect(() => validateTestRequest(students, JSON.stringify({ name: '中'.repeat(171) }))).toThrow(/512/)
  expect(() => validateTestRequest(students, ' '.repeat(65537) + '{}')).toThrow(/65536/)
  expect(() => validateTestRequest(grade, JSON.stringify({ ...base, score_min: 2, score_max: 1 }))).toThrow(/score_max/)
  expect(validateTestRequest(grade, JSON.stringify({ ...base, score_min: -1, score_max: 101 })).score_min).toBe(-1)
  expect(validateTestRequest(grade, JSON.stringify({ ...base, score_max: 1e20 })).score_max).toBe(1e20)
  expect(validateTestRequest(students, '{"page":null,"student_number":" 0001 "}').student_number).toBe(' 0001 ')
  expect(validateTestRequest(api('organizations.query'), '{"is_active":false}').is_active).toBe(false)
  for (const id of ['students.timetable.query', 'staff.timetable.query']) {
    const captured = backendCapture(id).request
    for (const change of [{ academic_year: 'all' }, { academic_year: '2026-2028' }, { semester: '3' }, { page: 1 }]) expect(() => validateTestRequest(api(id), JSON.stringify({ ...captured, ...change }))).toThrow()
  }
})

test('source examples expose only editable paging while context remains bound to backend identity', () => {
  for (const source of catalog.filter(item => item.kind === 'source')) {
    const text = editableExample(source, JSON.stringify(source.request_example))
    expect(validateTestRequest(source, text)).toEqual({ page: 1, page_size: 10 })
    expect(validateTestRequest(source, '{"page":0,"page_size":null}')).toEqual({ page: 0, page_size: null })
    expect(() => validateTestRequest(source, '{"page_size":101}')).toThrow()
    expect(() => validateTestRequest(source, '{"api_id":"wrong"}')).toThrow()
    expect(testParameters(source).every(field => !field.advanced)).toBe(true)
  }
  expect(testParameters(api('students.query')).find(field => field.name === 'student_number')?.advanced).toBe(false)
  expect(testParameters(api('students.query')).find(field => field.name === 'study_years')?.advanced).toBe(true)
})
