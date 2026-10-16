import { test, expect } from '@playwright/test'
import { backendCapture, sourceResponse } from '../fixtures/backend'
import { formatResponseJSON } from '../src/shared/formatResponseJSON'
const tokens = (text: string) => text.match(/"(?:\\[\s\S]|[^"\\])*"|[^\s]/g)?.join('')
test('formats recorded business and upstream responses without changing tokens', () => {
  for (const text of [JSON.stringify(backendCapture('students.grades.query').response), sourceResponse('dwd_bzksjbsjzlb')]) {
    const formatted = formatResponseJSON(text)!
    expect(formatted).not.toBeNull()
    expect(formatted).toContain('\n  "')
    expect(tokens(formatted)).toBe(tokens(text))
    expect(formatResponseJSON(formatted)).toBe(formatted)
  }
})
test('preserves exact numbers, escaped strings, duplicate fields and empty containers', () => {
  // JSON protocol boundaries, not normal business mock rows.
  const text = '{"n":9007199254740993,"n":1.2300e+40,"s":"{,}: \\"中文\\\\path","a":[],"b":{},"x":[true,null,-0]}'
  const formatted = formatResponseJSON(text)!
  expect(formatted).toContain('9007199254740993')
  expect(formatted).toContain('1.2300e+40')
  expect(formatted).toContain('-0')
  expect(formatted).toContain('"a": []')
  expect(formatted).toContain('"b": {}')
  expect(tokens(formatted)).toBe(tokens(text))
  expect(formatResponseJSON(' [  ] ')).toBe('[]')
  expect(formatResponseJSON(' "literal" ')).toBe('"literal"')
})
test('invalid, incomplete and excessively large or deep JSON remains raw', () => {
  for (const text of ['<h1>Bad Gateway</h1>', '{"data":', '', '{"a":1,}', '['.repeat(65) + '0' + ']'.repeat(65), ' '.repeat(2 * 1024 * 1024 + 1)]) {
    expect(formatResponseJSON(text)).toBeNull()
  }
})
