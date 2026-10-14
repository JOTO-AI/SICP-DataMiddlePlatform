import { test, expect } from '@playwright/test'
import { backendCatalog } from '../fixtures/backend'
import { readCatalog, filterCatalog, type CatalogAPI } from '../src/app/catalog'

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
