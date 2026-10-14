export function pollingDelay(interval: number, failures: number, retryAfter?: string, now = Date.now()) {
  const backoff = Math.min(60000, interval * 2 ** Math.min(6, Math.max(0, failures)))
  if (!retryAfter) return backoff
  const serverDelay = /^\d+$/.test(retryAfter) ? Number(retryAfter) * 1000 : Date.parse(retryAfter) - now
  return Number.isFinite(serverDelay) ? Math.max(backoff, Math.min(300000, Math.max(0, serverDelay))) : backoff
}
