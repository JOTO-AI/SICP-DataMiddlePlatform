import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { filterSources, sourceItems } from '../src/app/sources'

test('source filters preserve the backend example and cannot invent missing rows', () => {
  const data = managementCapture('sources').response.data
  const items = sourceItems(data)!
  expect(items).toHaveLength(1) // selected_items capture, not a fictional full 14-source snapshot
  expect(filterSources(items, ` ${items[0]!.id.toUpperCase()} `, 'none')).toEqual(items)
  expect(filterSources(items, '', 'unresolved')).toEqual([])
  expect(filterSources(items, 'no matching source', '')).toEqual([])
  expect(items[0]!.confirmed_until).toBeNull()
  expect(items[0]!.window).toBeNull()
  expect(sourceItems(null)).toBeNull()
  expect(sourceItems({ items: [items[0], items[0]] })).toBeNull()
})
