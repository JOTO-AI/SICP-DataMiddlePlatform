import { test, expect } from '@playwright/test'
import { backendCapture, backendCatalog, sourceResponse } from '../fixtures/backend'

test('backend fixture copies preserve original identities and do not leak mutations', () => {
  const first = backendCapture('students.query')
  const data = first.response.data as { items: { student_number: string; name: string }[] }
  const original = data.items[0].name
  expect(data.items[0].student_number).toBe('00202600001')
  data.items[0].name = 'modified by this test'
  const second = backendCapture('students.query').response.data as typeof data
  expect(second.items[0].name).toBe(original)
  expect(() => backendCapture('not-an-operation')).toThrow('No recorded backend fixture')
})

test('source fixtures keep existing raw responses and catalog unknown states', () => {
  const catalog = backendCatalog() as { id: string; kind: string; readiness: string }[]
  expect(catalog).toHaveLength(23)
  for (const source of catalog.filter(api => api.kind === 'source')) {
    expect(source.readiness).toBe('unknown')
    expect(JSON.parse(sourceResponse(source.id)).data.list.length).toBeGreaterThan(0)
    expect(JSON.parse(sourceResponse(source.id, 'empty')).data.list).toEqual([])
  }
  expect(() => sourceResponse('../.env')).toThrow('No recorded source fixture')
})
