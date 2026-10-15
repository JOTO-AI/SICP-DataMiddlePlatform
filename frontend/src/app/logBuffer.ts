import type { LogEvent } from './logHistory'
export interface LogBuffer { visible: LogEvent[]; pending: LogEvent[]; received: number; dropped: number }
export const emptyLogBuffer = (): LogBuffer => ({ visible: [], pending: [], received: 0, dropped: 0 })
const budget = (limit: number) => Number.isSafeInteger(limit) ? Math.max(0, Math.min(1000, limit)) : 0
export function trimLogBuffer(buffer: LogBuffer, limit: number) {
  const max = budget(limit)
  if (buffer.visible.length > max) { buffer.dropped += buffer.visible.length - max; buffer.visible.length = max }
  const room = max - buffer.visible.length
  if (buffer.pending.length > room) { buffer.dropped += buffer.pending.length - room; buffer.pending.length = room }
}
export function appendLog(buffer: LogBuffer, item: LogEvent, limit: number) {
  buffer.received++; buffer.pending.unshift(item); trimLogBuffer(buffer, limit)
}
export function revealLogs(buffer: LogBuffer, limit: number) {
  // Keep at least half the budget available for arrivals while existing rows are being read.
  const max = Math.min(budget(limit), Math.max(1, Math.floor(budget(limit) / 2))), merged = [...buffer.pending, ...buffer.visible]
  buffer.dropped += Math.max(0, merged.length - max)
  buffer.visible = merged.slice(0, max); buffer.pending = []
}