import { session } from '../session'

export interface RequestRun { signal: AbortSignal; current: () => boolean }
export function createRequestScope() {
  let generation = 0
  let controller: AbortController | undefined
  function cancel() { generation++; controller?.abort(); controller = undefined }
  function begin(): RequestRun {
    cancel()
    const id = generation, epoch = session.epoch
    const next = new AbortController()
    controller = next
    return { signal: next.signal, current: () => id === generation && epoch === session.epoch && !next.signal.aborted }
  }
  return { begin, cancel }
}
