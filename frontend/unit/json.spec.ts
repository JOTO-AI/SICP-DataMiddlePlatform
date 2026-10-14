import { test, expect } from '@playwright/test'
import { backendCapture, sourceResponse } from '../fixtures/backend'
import { parseRequestObject, parseEditableJSON, boundedText, parameterError } from '../src/shared/json'

test('request conversion preserves real backend query parameters and refuses imprecise numbers', () => {
  for (const operation of ['students.query', 'students.grades.query', 'organizations.query', 'staff.timetable.query']) {
    const request = backendCapture(operation).request
    expect(parseRequestObject(JSON.stringify(request))).toEqual(request)
  }
  expect(parseEditableJSON('"00202600001"')).toBe('00202600001')
  // Protocol primitives and invalid payloads, not fabricated business rows.
  expect(parseRequestObject('{"zero":0,"flag":false,"missing":null,"values":[]}')).toEqual({ zero: 0, flag: false, missing: null, values: [] })
  expect(() => parseRequestObject('{"revision":9007199254740993}')).toThrow('无法精确转换')
  expect(() => parseRequestObject('[]')).toThrow('JSON 对象')
  expect(() => parseRequestObject(' '.repeat(262145))).toThrow('262144')
  expect(parameterError(null, { name: 'x', type: 'string', nullable: true })).toBe('')
  expect(parameterError(false, { name: 'x', type: 'boolean' })).toBe('')
})

test('JSON display bounds real source text and does not split a surrogate pair', () => {
  const text = sourceResponse('dwd_bzksjbsjzlb')
  for (const expanded of [false, true]) {
    const result = boundedText(text, expanded)
    expect(result.text).toBe(text.slice(0, result.text.length))
    expect(result.text.length).toBeLessThanOrEqual(expanded ? 65536 : 4096)
    expect(result.truncated).toBe(result.text.length < text.length)
  }
  expect(boundedText('a'.repeat(4095) + '😀', false).text.length).toBe(4095)
})
