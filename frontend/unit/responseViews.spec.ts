import { test, expect } from '@playwright/test'
import { backendCatalog, backendCapture } from '../fixtures/backend'
import { sourceJobResult } from '../fixtures/sourceTests'
import { responseRecords, responseColumns, recordCell, rawField, organizationNodes, datedLessons } from '../src/app/responseViews'
import { arrayTokens, objectTokens } from '../src/shared/jsonObject'
import type { CatalogAPI } from '../src/app/catalog'
test('all recorded business and source responses produce bounded views from the same raw records', () => {
  for (const api of backendCatalog() as CatalogAPI[]) {
    const raw = api.kind === 'business' ? JSON.stringify(backendCapture(api.id).response) : sourceJobResult(api.id).raw
    const records = responseRecords(raw, api.kind)
    const expected = api.kind === 'business' ? JSON.parse(raw).data.items : JSON.parse(raw).data.result.list
    expect(records.error).toBe(''); expect(records.objects).toEqual(expected)
    expect(records.rows).toHaveLength(expected.length)
    expect(records.truncated).toBe(false)
    expect(responseColumns(api, records).length).toBeGreaterThan(0)
    for (const row of records.rows) for (const [key, token] of row) {
      expect(rawField(row, key)).toBe(token)
      if (/^-?\d+(\.\d+)?$/.test(token)) expect(recordCell(row, key)).toBe(token)
    }
  }
})
test('raw table cells preserve protocol primitives and escaped separators without numeric normalization', () => {
  // Scalar protocol boundaries only; not invented business records.
  const text = '[{"id":"00202600001","huge":9007199254740993,"zero":0,"flag":false,"nil":null,"empty":"","text":"a,]\\\"b"}]'
  JSON.parse(text)
  const tokens = arrayTokens(text), row = objectTokens(tokens.values[0]!)
  expect(recordCell(row, 'id')).toBe('00202600001')
  expect(recordCell(row, 'huge')).toBe('9007199254740993')
  expect(recordCell(row, 'zero')).toBe('0'); expect(recordCell(row, 'flag')).toBe('false')
  expect(recordCell(row, 'nil')).toBe('null（未提供）'); expect(recordCell(row, 'empty')).toBe('空字符串')
  expect(recordCell(row, 'absent')).toBe('未返回'); expect(recordCell(row, 'text')).toBe('a,]"b')
  expect(arrayTokens(text, 0)).toEqual({ values: [], truncated: true })
  expect(responseRecords('{"code":"ok","data":null}', 'business').error).not.toBe('')
})
test('organization view preserves every recorded node and marks missing or cyclic parents without synthetic nodes', () => {
  const rows = responseRecords(JSON.stringify(backendCapture('organizations.query').response), 'business').objects
  const tree = organizationNodes(rows)
  expect(tree).toHaveLength(rows.length)
  expect(new Set(tree.map(node => node.index)).size).toBe(rows.length)
  expect(rows[tree[0]!.index]!.organization_code).toBe('10001')
  expect(tree.find(node => rows[node.index]!.organization_code === '00D3')?.depth).toBe(2)
  const partial = rows.filter(row => row.organization_code !== '10001')
  expect(organizationNodes(partial).filter(node => node.note.includes('上级未')).length).toBe(2)
  const cycle = structuredClone(rows)
  cycle.find(row => row.organization_code === '10001')!.parent_organization_code = '00D3'
  expect(organizationNodes(cycle)).toHaveLength(rows.length)
  expect(organizationNodes(cycle).some(node => node.note.includes('闭环'))).toBe(true)
})
test('date view uses only actual timetable dates and retains unmappable records for the table', () => {
  for (const id of ['students.timetable.query', 'staff.timetable.query']) {
    const rows = responseRecords(JSON.stringify(backendCapture(id).response), 'business').objects
    const dated = datedLessons(rows)
    expect(dated.groups).toHaveLength(1)
    expect(dated.groups[0]![0]).toBe('2026-09-07')
    expect(dated.groups[0]![1]).toHaveLength(rows.length)
    expect(dated.unmapped).toEqual([])
    const missing = structuredClone(rows); missing[0]!.dates = null
    expect(datedLessons(missing).unmapped).toEqual([0])
    expect(missing).toHaveLength(rows.length)
  }
})
