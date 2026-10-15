import { readLogEvent, type LogEvent, type LogPage } from './logHistory'
import { timestampMicros } from '../shared/timestamp'
export interface SSEFrame { event: string; id: string; data: string }
export function createSSEParser(emit: (frame: SSEFrame) => void) {
  let buffer = '', event = '', id = '', lines: string[] = [], size = 0
  const line = (text: string) => {
    size += new TextEncoder().encode(text).byteLength + 1
    if (size > 270336) throw new Error('实时日志帧超过预算。')
    if (!text) {
      if (lines.length) emit({ event: event || 'message', id, data: lines.join('\n') })
      event = ''; id = ''; lines = []; size = 0; return
    }
    if (text.startsWith(':')) return
    const colon = text.indexOf(':'), field = colon < 0 ? text : text.slice(0, colon)
    let value = colon < 0 ? '' : text.slice(colon + 1); if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') event = value
    if (field === 'id') { if (value.includes('\0') || value.length > 4096) throw new Error('实时日志帧标识无效。'); id = value }
    if (field === 'data') lines.push(value)
  }
  return { push(text: string) {
    buffer += text
    let start = 0
    for (let i = 0; i < buffer.length; i++) {
      const c = buffer[i]
      if (c !== '\r' && c !== '\n') continue
      if (c === '\r' && i === buffer.length - 1) break
      line(buffer.slice(start, i)); if (c === '\r' && buffer[i + 1] === '\n') i++
      start = i + 1
    }
    buffer = buffer.slice(start)
    if (buffer.length + size > 270336) throw new Error('实时日志未完成帧超过预算。')
  } }
}
export interface StreamControl { code: string; retryable: boolean; rebuild_required: boolean }
export function createLogStreamState(page: LogPage) {
  let cursor = page.stream_cursor!, position = BigInt(page.high_water)
  const seen = new Set(page.items.map(i => `${i.source}/${i.epoch}/${i.event_id}`))
  return {
    get cursor() { return cursor }, get position() { return position.toString() },
    accept(frame: SSEFrame): { item?: LogEvent; control?: StreamControl } {
      if (!['event', 'checkpoint', 'control'].includes(frame.event)) return {}
      const value = JSON.parse(frame.data)
      if (frame.event === 'control') {
        if (!value || typeof value.code !== 'string' || !/^[a-z_]{1,64}$/.test(value.code) || typeof value.retryable !== 'boolean' || typeof value.rebuild_required !== 'boolean') throw new Error('实时控制消息无效。')
        return { control: value }
      }
      if (!frame.id || frame.id.length > 4096 || /[\r\n\x00]/.test(frame.id)) throw new Error('实时消息缺少有效恢复位置。')
      if (frame.event === 'checkpoint') {
        if (!value || typeof value.sequence !== 'string' || !/^(0|[1-9]\d{0,18})$/.test(value.sequence) || BigInt(value.sequence) < position) throw new Error('实时检查点位置倒退或无效。')
        position = BigInt(value.sequence); cursor = frame.id; return {}
      }
      const item = readLogEvent(value, page.source, page.epoch), f = page.filter
      if (!item || item.occurred_at === null || timestampMicros(item.occurred_at)! < timestampMicros(f.from)! || ['kind', 'phase', 'outcome', 'operation', 'request_id', 'task_id'].some(key => f[key] && f[key] !== (item as unknown as Record<string, unknown>)[key]) || f.test_run_id && f.test_run_id !== item.test?.test_run_id) throw new Error('实时事件身份、世代或范围不匹配。')
      const key = `${item.source}/${item.epoch}/${item.event_id}`, sequence = BigInt(item.sequence)
      if (sequence <= position) { if (seen.has(key)) return {}; throw new Error('实时事件位置倒退，请重查历史。') }
      if (seen.has(key)) throw new Error('相同事件号出现不同位置。')
      seen.add(key); if (seen.size > 1000) seen.delete(seen.values().next().value!)
      position = sequence; cursor = frame.id; return { item }
    },
  }
}