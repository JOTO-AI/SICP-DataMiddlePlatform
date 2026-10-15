import { session, sessionRequestSignal, clearSession } from '../session'
import { logStreamPath } from '../app/logLive'
import { createSSEParser, type SSEFrame } from '../app/logSSE'
import type { LogSource } from '../app/logHistory'
export class LogStreamError extends Error {
  constructor(message: string, public status = 0, public retryAfter = 0) { super(message) }
}
export async function readLogStream(source: LogSource, cursor: string, signal: AbortSignal, receive: (frame: SSEFrame) => void) {
  const epoch = session.epoch, idle = new AbortController(), combined = AbortSignal.any([signal, sessionRequestSignal(), idle.signal])
  let timer: ReturnType<typeof setTimeout> | undefined, reader: ReadableStreamDefaultReader<Uint8Array> | undefined
  const renew = () => { clearTimeout(timer); timer = setTimeout(() => idle.abort(), 45000) }
  const current = () => !combined.aborted && epoch === session.epoch && !!session.operator
  if (!current()) return
  renew()
  try {
    const response = await fetch(logStreamPath(source, cursor), { headers: { Accept: 'text/event-stream', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.csrf, 'Last-Event-ID': cursor }, credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal: combined })
    if (!current()) return
    if (response.status === 401) { clearSession('登录已过期，请重新登录。'); return }
    if (!response.ok) { const raw = response.headers.get('Retry-After') ?? ''; throw new LogStreamError('实时日志连接暂不可用。', response.status, /^\d{1,5}$/.test(raw) ? Math.min(Number(raw), 86400) * 1000 : 0) }
    if (!response.headers.get('Content-Type')?.toLowerCase().startsWith('text/event-stream') || !response.body) throw new LogStreamError('实时接口未返回事件流。', 415)
    const parser = createSSEParser(frame => { if (current()) receive(frame) }), decoder = new TextDecoder('utf-8', { fatal: true })
    reader = response.body.getReader()
    while (current()) {
      const result = await reader.read()
      if (!current()) return
      if (result.done) { decoder.decode(); return }
      renew(); try { parser.push(decoder.decode(result.value, { stream: true })) } catch { throw new LogStreamError('实时日志帧无法校验，请重新读取历史。', 422) }
    }
  } finally { clearTimeout(timer); if (reader) { try { await reader.cancel() } catch {} reader.releaseLock() } }
}