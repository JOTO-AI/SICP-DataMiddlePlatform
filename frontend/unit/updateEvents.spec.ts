import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { recordedUpdateEvent } from '../fixtures/updateTests'
import { updateListPath, updateDetailPath, readUpdatePage, readUpdateDetail } from '../src/app/updateEvents'
test('update events preserve captured task facts and backend event-to-task identity', () => {
  const captured = managementCapture('update_detail').response.data as Record<string, unknown>, event = recordedUpdateEvent()
  expect(event.id).toBe(captured.task_id); expect(event.task_id).toBe(captured.task_id)
  expect(readUpdatePage(managementCapture('updates').response.data)).toEqual(managementCapture('updates').response.data)
  const page = readUpdatePage({ items: [event], next_cursor: null })!
  expect(page.items[0]).toEqual(event); expect(page.items[0]!.finished_at).toBeNull()
  const detail = readUpdateDetail(captured, String(captured.task_id))!
  expect(detail.progress_confirmed).toBe(false); expect(detail.started_at).toBeNull()
  expect(detail).toEqual(captured)
})
test('update paths only send current supported pagination fields and reject ambiguous identities', () => {
  expect(updateListPath()).toBe('/admin/v1/update-events?page_size=50')
  const url = new URL(updateListPath('protocol-cursor+/='), 'https://console.invalid')
  expect([...url.searchParams.keys()]).toEqual(['cursor']); expect(url.searchParams.get('cursor')).toBe('protocol-cursor+/=')
  expect(() => updateListPath('x'.repeat(4097))).toThrow()
  expect(updateDetailPath(String(recordedUpdateEvent().id))).toBe(managementCapture('update_detail').path)
  expect(() => updateDetailPath('../tasks')).toThrow()
  expect(() => updateDetailPath('id?override=true')).toThrow()
})
test('update readers reject conflicting IDs, duplicate pages and malformed metadata without fabricating defaults', () => {
  const event = recordedUpdateEvent(), captured = managementCapture('update_detail').response.data as Record<string, unknown>
  expect(readUpdatePage({ items: [{ ...event, task_id: 'protocol-mismatch' }], next_cursor: null })).toBeNull()
  expect(readUpdatePage({ items: [event, event], next_cursor: null })).toBeNull()
  expect(readUpdatePage({ items: [event], next_cursor: '' })).toBeNull()
  expect(readUpdatePage({ items: [event] })).toBeNull()
  expect(readUpdateDetail(captured, 'protocol-mismatch')).toBeNull()
  expect(readUpdateDetail({ ...captured, progress_confirmed: 'false' }, String(event.id))).toBeNull()
  expect(readUpdateDetail({ ...captured, finished_at: 'not-a-time' }, String(event.id))).toBeNull()
  expect(readUpdateDetail({ ...captured, diagnostics_unavailable: 'false' }, String(event.id))).toBeNull()
  const unknown = readUpdateDetail({ ...captured, status: 'future_status' }, String(event.id))!
  expect(unknown.status).toBe('future_status')
})
test('update filters use server predicates and only the bound cursor on continuation', () => {
  const e = recordedUpdateEvent()
  const f = { from: '2026-10-12T00:00:00Z', to: '2026-10-14T00:00:00Z', mode: String(e.mode), status: String(e.status), task_id: String(e.task_id) }
  const q = new URL(updateListPath('', f), 'https://console.invalid').searchParams
  expect(readUpdatePage({ items: [e], next_cursor: null, filter: f })).not.toBeNull()
  expect(readUpdatePage({ items: [e], next_cursor: null, filter: { ...f, status: 'failed' } })).toBeNull()
  expect(q.get('task_id')).toBe(e.task_id); expect(q.get('mode')).toBe(e.mode); expect(q.get('status')).toBe(e.status)
  expect(q.get('from')).toBe('2026-10-12T00:00:00.000Z')
  expect(updateListPath('bound-cursor', f)).toBe('/admin/v1/update-events?cursor=bound-cursor')
  expect(() => updateListPath('', { ...f, task_id: '../task' })).toThrow()
  expect(() => updateListPath('', { ...f, to: '' })).toThrow()
  expect(() => updateListPath('', { ...f, from: '2026-10-01T00:00:00Z' })).toThrow()
})
