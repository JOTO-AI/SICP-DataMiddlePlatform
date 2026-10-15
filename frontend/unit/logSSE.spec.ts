import { test, expect } from '@playwright/test'
import { recordedLogPage } from '../fixtures/logTests'
import { createLogStreamState, createSSEParser, type SSEFrame } from '../src/app/logSSE'
function setup() {
  const page = recordedLogPage().data, item = page.items[0]
  page.filter.mode = 'live'; page.filter.to = null; page.stream_cursor = 'initial'; page.high_water = '0'; page.items = []
  return { page, item, frame: `id: complete-event\r\nevent: event\r\ndata: ${JSON.stringify(item)}\r\n\r\n` }
}
test('SSE chunk boundaries, CRLF and complete frames preserve recorded event and checkpoint positions', () => {
  const { page, item, frame } = setup(), state = createLogStreamState(page), events: unknown[] = []
  const parser = createSSEParser(f => { const result = state.accept(f); if (result.item) events.push(result.item) })
  for (const char of frame.slice(0, -1)) parser.push(char)
  expect(events).toEqual([]); expect(state.cursor).toBe('initial')
  parser.push(frame.slice(-1)); expect(events).toEqual([item]); expect(state.cursor).toBe('complete-event')
  parser.push(frame); expect(events).toHaveLength(1)
  parser.push(': heartbeat\n\nid: must-not-advance\nevent: control\ndata: {"code":"connection_expired","retryable":true,"rebuild_required":false}\n\n')
  expect(state.cursor).toBe('complete-event')
  parser.push('id: checkpoint-next\nevent: checkpoint\ndata: {"sequence":"22"}\n\n')
  expect(state.cursor).toBe('checkpoint-next'); expect(state.position).toBe('22')
  parser.push('id: partial\nevent: checkpoint\ndata: {"sequence":"23"}')
  expect(state.cursor).toBe('checkpoint-next')
})
test('SSE malformed frames cannot advance and oversized incomplete frames are bounded', () => {
  const { page, item } = setup(), state = createLogStreamState(page)
  for (const data of [{ ...item, epoch: 'changed' }, { ...item, source: 'upstream' }]) expect(() => state.accept({ event: 'event', id: 'rejected', data: JSON.stringify(data) })).toThrow()
  expect(state.cursor).toBe('initial')
  expect(() => state.accept({ event: 'checkpoint', id: '', data: '{"sequence":"1"}' })).toThrow()
  expect(() => createSSEParser(() => {}).push('data: ' + 'x'.repeat(270337))).toThrow()
  const frames: SSEFrame[] = [], parser = createSSEParser(f => frames.push(f))
  parser.push('id: multi\nevent: checkpoint\ndata: {\ndata: "sequence":"21"}\n\n')
  state.accept(frames[0]!); expect(state.position).toBe('21')
})