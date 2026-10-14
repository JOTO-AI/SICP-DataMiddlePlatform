export class ReadQueueFullError extends Error {}
export function createReadLimiter(limit = 4, queueLimit = 16) {
  let active = 0
  interface Waiter { signal: AbortSignal; resolve: (release: () => void) => void; reject: (error: Error) => void; cancel: () => void }
  const queue: Waiter[] = []
  function permit() {
    active++
    let released = false
    return () => {
      if (released) return
      released = true; active--
      const next = queue.shift()
      if (next) { next.signal.removeEventListener('abort', next.cancel); next.resolve(permit()) }
    }
  }
  function acquire(signal: AbortSignal): (() => void) | Promise<() => void> {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    if (active < limit) return permit()
    if (queue.length >= queueLimit) throw new ReadQueueFullError('读取请求过多，请稍后重试。')
    return new Promise((resolve, reject) => {
      const waiter: Waiter = { signal, resolve, reject, cancel: () => {
        const index = queue.indexOf(waiter)
        if (index >= 0) queue.splice(index, 1)
        signal.removeEventListener('abort', waiter.cancel)
        reject(new DOMException('Aborted', 'AbortError'))
      } }
      queue.push(waiter)
      signal.addEventListener('abort', waiter.cancel, { once: true })
    })
  }
  return { acquire }
}
export const readLimiter = createReadLimiter()
