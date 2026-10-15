import { test, expect } from '@playwright/test'
import { backendCatalog } from '../fixtures/backend'
import { diagnosticCapture } from '../fixtures/diagnostics'
import type { CatalogAPI } from '../src/app/catalog'
import { catalogPurpose, catalogNote, catalogLimit } from '../src/app/catalogCopy'
import { schemaRows } from '../src/app/catalogSchema'
import { testParameters } from '../src/app/testRequest'
import { milliseconds } from '../src/shared/duration'

test('recorded catalog documentation is Chinese and required parameters lead without changing metadata', () => {
  for (const api of backendCatalog() as CatalogAPI[]) {
    const before = JSON.stringify(api)
    expect(catalogPurpose(api), api.id).toMatch(/[\u4e00-\u9fff]/)
    for (const note of api.notes) expect(catalogNote(note), api.id).toMatch(/[\u4e00-\u9fff]/)
    for (const schema of [api.request, api.response]) {
      const rows = schemaRows(api, schema).rows
      expect(rows.some(row => row.path === '$')).toBe(false)
      for (const row of rows) expect(row.limit, api.id + ' ' + row.path).toMatch(/[\u4e00-\u9fff]/)
    }
    const required = Object.entries(api.request.properties ?? {}).filter(([, f]) => f.required).map(([name]) => name)
    expect(schemaRows(api, api.request).rows.filter(row => !row.path.includes('.') && !row.path.includes('[]')).slice(0, required.length).map(row => row.path)).toEqual(required)
    const editor = testParameters(api)
    expect(editor.slice(0, required.length).map(row => row.name)).toEqual(required)
    expect(editor.filter(row => row.required).every(row => !row.advanced)).toBe(true)
    expect(JSON.stringify(api)).toBe(before)
  }
})
test('unrecognized documentation stays visible instead of silently borrowing outdated translations', () => {
  const api = (backendCatalog() as CatalogAPI[])[0]!
  expect(catalogPurpose({ ...api, purpose: 'future backend wording' })).toBe('future backend wording')
  expect(catalogNote('future note')).toBe('future note')
  expect(catalogLimit('future constraint')).toBe('future constraint')
  expect(catalogLimit('1..100')).toBe('范围：1–100')
})
test('captured request durations display milliseconds while preserving large and submillisecond boundaries', () => {
  const captured = diagnosticCapture('initial_token_502')
  for (const request of captured.attempt.diagnostics) {
    expect(Number(milliseconds(request.duration_ns))).toBe(request.duration_ns! / 1e6)
  }
  // Numeric formatting boundaries, not invented business or log records.
  expect(milliseconds('12345678901234567890')).toBe('12345678901234.56789')
  expect(milliseconds('1')).toBe('0.000001')
  expect(milliseconds(0)).toBe('0')
  expect(milliseconds('001000000')).toBe('1')
  expect(milliseconds(null)).toBe('未记录')
  expect(milliseconds(undefined, '未观测')).toBe('未观测')
  expect(milliseconds(-1)).toBe('数值无法精确显示')
  expect(milliseconds(Number.MAX_SAFE_INTEGER + 1)).toBe('数值无法精确显示')
  expect(milliseconds('1e6')).toBe('无效耗时')
})

import { parameterSchemaRows } from '../src/app/catalogSchema'
import { sourceNames } from '../src/app/sourceNames'
test('parameter array elements retain their constraints without becoming separate required fields', () => {
  const api = (backendCatalog() as CatalogAPI[]).find(item => item.id === 'students.grades.batch-query')!
  const rows = parameterSchemaRows(api).rows
  expect(rows.some(row => row.path === 'student_numbers[]')).toBe(false)
  expect(rows.find(row => row.path === 'student_numbers')).toMatchObject({ type: 'string[]', required: '否' })
  expect(rows.find(row => row.path === 'student_numbers')?.limit).toContain('512')
  expect(rows[0]?.path).toBe('academic_year')
  expect(Object.keys(sourceNames).sort()).toEqual((backendCatalog() as CatalogAPI[]).filter(item => item.kind === 'source').map(item => item.id).sort())
})
