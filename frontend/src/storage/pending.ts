import type { PendingOperation } from '../api/operations'

const storageKey = 'sicp.pending-operations.v1'
export const recoveryTTL = 15 * 60 * 1000
export interface RecoveryContext { environment: string; subject: string }
export interface PendingRecovery extends RecoveryContext { operation: PendingOperation; createdAt: number; expiresAt: number; location?: string }
type StoragePort = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
function browserStorage(): StoragePort | undefined {
  try { return typeof window === 'undefined' ? undefined : window.sessionStorage } catch { return undefined }
}
// An independent allowlist prevents future form fields or responses from reaching storage.
function safeOperation(value: unknown): value is PendingOperation {
  if (!value || typeof value !== 'object') return false
  const op = value as PendingOperation
  if (typeof op.path !== 'string' || typeof op.key !== 'string' || typeof op.payload !== 'string' || op.payload.length > 2048) return false
  if (!/^[\x21-\x7e]{1,128}$/.test(op.key) || op.key.includes(',')) return false
  if (op.kind === 'task') return /^\/admin\/v1\/(tasks\/(incremental|force-full|[A-Za-z0-9_-]+\/rerun)|tables\/dwd_[a-z0-9_]+\/force-full)$/.test(op.path) && op.payload === '{}'
  if (op.kind !== 'job' || !/^[A-Za-z0-9_:-]{1,128}$/.test(op.key)) return false
  const payload: unknown = JSON.parse(op.payload)
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return false
  const body = payload as Record<string, unknown>
  const allowed: Record<string, string[]> = {
    '/admin/v1/api-tests': ['api_id', 'version', 'page', 'page_size', 'idempotency_key'],
    '/admin/v1/notifications/test': ['revision', 'idempotency_key'],
    '/admin/v1/maintenance/cleanup': ['preview_id', 'revision', 'idempotency_key'],
  }
  const keys = allowed[op.path]
  if (!keys || body.idempotency_key !== op.key || Object.keys(body).some(key => !keys.includes(key))) return false
  return Object.entries(body).every(([key, value]) => key === 'page' || key === 'page_size'
    ? Number.isSafeInteger(value) && Number(value) > 0
    : typeof value === 'string' && /^[A-Za-z0-9_.:-]{1,128}$/.test(value))
}
function safeLocation(op: PendingOperation, location?: string) {
  if (location === undefined) return true
  const prefix = op.kind === 'task' ? '/admin/v1/tasks/' : op.path === '/admin/v1/notifications/test' ? '/admin/v1/notifications/tests/' : `${op.path}/`
  return location.startsWith(prefix) && /^[A-Za-z0-9_-]+$/.test(location.slice(prefix.length))
}
export function clearPendingRecovery(storage = browserStorage()) { try { storage?.removeItem(storageKey) } catch { /* unavailable storage has no usable recovery state */ } }
export function loadPendingRecovery(context: RecoveryContext, storage = browserStorage(), now = Date.now()): PendingRecovery[] {
  try {
    const raw = storage?.getItem(storageKey)
    if (!raw) return []
    if (raw.length > 32768) throw new Error('Oversized recovery state')
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed) || parsed.length > 8) throw new Error('Invalid recovery state')
    const valid = parsed.filter((entry): entry is PendingRecovery => entry && entry.environment === context.environment && entry.subject === context.subject &&
      Number.isSafeInteger(entry.createdAt) && Number.isSafeInteger(entry.expiresAt) && entry.createdAt <= now && entry.expiresAt > now && entry.expiresAt <= entry.createdAt + recoveryTTL &&
      safeOperation(entry.operation) && safeLocation(entry.operation, entry.location))
      .map(entry => ({ environment: context.environment, subject: context.subject, createdAt: entry.createdAt, expiresAt: entry.expiresAt,
        operation: { kind: entry.operation.kind, path: entry.operation.path, key: entry.operation.key, payload: entry.operation.payload }, ...(entry.location ? { location: entry.location } : {}) }))
    if (valid.length) storage?.setItem(storageKey, JSON.stringify(valid))
    else clearPendingRecovery(storage)
    return valid
  } catch { clearPendingRecovery(storage); return [] }
}
export function savePendingRecovery(context: RecoveryContext, operation: PendingOperation, location?: string, storage = browserStorage(), now = Date.now()): boolean {
  try {
    if (!storage || !context.environment || !context.subject || !safeOperation(operation) || !safeLocation(operation, location)) return false
    const entries = loadPendingRecovery(context, storage, now)
    const same = (entry: PendingRecovery) => entry.operation.kind === operation.kind && entry.operation.path === operation.path && entry.operation.key === operation.key
    const prior = entries.find(same)
    if (prior && prior.operation.payload !== operation.payload) return false
    if (!prior && entries.length >= 8) return false
    const entry: PendingRecovery = { environment: context.environment, subject: context.subject, createdAt: prior?.createdAt ?? now, expiresAt: prior?.expiresAt ?? now + recoveryTTL,
      operation: { kind: operation.kind, path: operation.path, key: operation.key, payload: operation.payload }, ...(location ? { location } : prior?.location ? { location: prior.location } : {}) }
    storage.setItem(storageKey, JSON.stringify([...entries.filter(item => !same(item)), entry]))
    return true
  } catch { return false }
}
export function removePendingRecovery(context: RecoveryContext, operation: PendingOperation, storage = browserStorage()) {
  try {
    const remaining = loadPendingRecovery(context, storage).filter(entry => entry.operation.kind !== operation.kind || entry.operation.path !== operation.path || entry.operation.key !== operation.key)
    if (remaining.length) storage?.setItem(storageKey, JSON.stringify(remaining))
    else clearPendingRecovery(storage)
  } catch { clearPendingRecovery(storage) }
}
