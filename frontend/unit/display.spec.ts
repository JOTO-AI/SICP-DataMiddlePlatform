import { test, expect } from '@playwright/test'
import { backendCapture } from '../fixtures/backend'
import { displayValue, displayEnum, displayTimestamp, fieldValue } from '../src/shared/display'

test('shared display preserves original identifiers, missing fields and protocol zero/false/empty distinctions', () => {
  const fixture = backendCapture('students.grades.query')
  const first = (fixture.response.data as { items: Record<string, unknown>[] }).items[0]!
  expect(displayValue(fieldValue(first, 'student.student_number'))).toBe('00202600001')
  expect(displayValue(fieldValue(first, 'course.attribute_name'))).toBe('未提供')
  expect(displayValue(first.score)).toBe('95')
  // Primitive boundary values only, never fabricated normal business rows.
  expect([0, false, null, []].map(displayValue)).toEqual(['0', '否', '未提供', '无'])
  expect(displayValue('9007199254740993')).toBe('9007199254740993')
  expect(displayEnum('__proto__', {})).toBe('未知状态（__proto__）')
  expect(displayTimestamp('0')).toBe('未知时间')
  expect(displayTimestamp(null)).toBe('未提供')
  expect(displayTimestamp(first.source_updated_at as string)).not.toMatch(/未知|Invalid|1970/)
})
