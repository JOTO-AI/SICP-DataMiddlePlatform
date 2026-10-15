import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { operationPath, readOperationPage, expandableOperation } from '../src/app/updateOperations'
test('operation tree consumes the recorded control node without inventing times or quantities', () => {
  const capture = managementCapture('operation_nodes'), page = readOperationPage(capture.response.data, '')!
  expect(page).toEqual(capture.response.data)
  expect(expandableOperation(page.items[0]!)).toBe(true)
  expect(page.items[0]!.started_at).toBeNull(); expect(page.items[0]!.count).toBeNull()
  const id = (managementCapture('update_detail').response.data as { task_id: string }).task_id
  expect(operationPath(id)).toBe(capture.path + '?page_size=50')
  const next = new URL(operationPath(id, 'publication', 'protocol-cursor'), 'https://console.invalid')
  expect([...next.searchParams.entries()]).toEqual([['cursor', 'protocol-cursor'], ['parent_operation_id', 'publication']])
})
test('operation tree rejects wrong parents and duplicate identities while keeping unknown facts null', () => {
  const p = managementCapture('operation_nodes').response.data as { items: Record<string, unknown>[]; next_cursor: null; parent_operation_id: string }
  expect(readOperationPage(p, 'publication')).toBeNull()
  expect(readOperationPage({ ...p, items: [...p.items, ...p.items] }, '')).toBeNull()
  for (const change of [{ count: 0 }, { generation: 9007199254740992 }, { started_at: 'invalid' }, { parent_operation_id: 'wrong' }, { id: 'x\n' }]) expect(readOperationPage({ ...p, items: [{ ...p.items[0], ...change }] }, '')).toBeNull()
  expect(readOperationPage({ items: [], next_cursor: null, parent_operation_id: 'publication' }, 'publication')).not.toBeNull()
  expect(() => operationPath('../task')).toThrow()
})