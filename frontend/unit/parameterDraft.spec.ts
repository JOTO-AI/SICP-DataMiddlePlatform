import { readFileSync } from 'node:fs'
import { test, expect } from '@playwright/test'
import { backendCapture, backendCatalog } from '../fixtures/backend'
import { testParameters, validateTestRequest } from '../src/app/testRequest'
import { parameterDraft, parameterDraftValue } from '../src/shared/parameterDraft'
import type { CatalogAPI } from '../src/app/catalog'

test('typed fields round-trip every real business request without JSON quotes', () => {
  for (const api of backendCatalog() as CatalogAPI[]) {
    if (api.kind !== 'business') continue
    const body = backendCapture(api.id).request
    const output: Record<string, unknown> = {}
    for (const field of testParameters(api)) {
      const row = parameterDraft(field.name, body, field)
      expect(parameterDraftValue(row)).toEqual(body[field.name])
      if (!row.present) continue
      // Simulate typing the same visible field contents.
      if (row.original !== null && row.text !== '') row.changed = true
      output[field.name] = parameterDraftValue(row)
      if (field.type === 'string') expect(row.text).toBe(body[field.name])
      if (field.type === 'array') expect(row.format).toBe('lines')
    }
    expect(output).toEqual(body)
    expect(validateTestRequest(api, JSON.stringify(output))).toEqual(body)
  }
})
test('text stays literal and clearing differs from untouched null or invalid examples', () => {
  const field = { name: 'student_number', type: 'string', nullable: true }
  const body = backendCapture('students.grades.query').request
  const row = parameterDraft(field.name, body, field)
  expect(row.text).toBe(body.student_number)
  // Protocol text boundary cases, not normal business mock rows.
  for (const text of ['0001', 'false', 'null', '"quoted"', 'a\\b', ' 001 ', '张三']) {
    row.text = text; row.changed = true
    expect(parameterDraftValue(row)).toBe(text)
    expect(JSON.parse(JSON.stringify({ value: parameterDraftValue(row) })).value).toBe(text)
  }
  row.text = ''
  expect(parameterDraftValue(row)).toBeUndefined()
  const nil = parameterDraft(field.name, { student_number: null }, field)
  expect(parameterDraftValue(nil)).toBeNull()
  nil.changed = true; nil.text = 'null'
  expect(parameterDraftValue(nil)).toBe('null')
  const invalid = parameterDraft(field.name, { student_number: false }, field)
  expect(invalid.format).toBe('json')
  expect(parameterDraftValue(invalid)).toBe(false)
  expect(parameterDraftValue(parameterDraft(field.name, { student_number: '' }, field))).toBe('')
})
test('list editing preserves each string and does not trim or drop blank entries', () => {
  const field = { name: 'student_numbers', type: 'array', itemType: 'string' }
  const body = JSON.parse(readFileSync(new URL('../../internal/query/contract/testdata/batch_grades_request.json', import.meta.url), 'utf8'))
  const row = parameterDraft(field.name, body, field)
  row.changed = true
  expect(parameterDraftValue(row)).toEqual(body.student_numbers)
  row.text = '001\n\n 002 '
  expect(parameterDraftValue(row)).toEqual(['001', '', ' 002 '])
})

test('boolean selection preserves the backend contract false value', () => {
  const body = JSON.parse(readFileSync(new URL('../../internal/query/contract/testdata/phase2.json', import.meta.url), 'utf8')).organizations_request
  const row = parameterDraft('is_active', body, { name: 'is_active', type: 'boolean' })
  expect(row.text).toBe('false'); row.changed = true
  expect(parameterDraftValue(row)).toBe(false)
})
