export interface LogContext { stage?: string; table?: string; instance?: string; slot?: number; attempt?: number; page?: number; mode?: string; operation?: string }
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
export function validLogContext(v: unknown): v is LogContext {
  if (!object(v)) return false
  return ['stage', 'table', 'instance', 'mode', 'operation'].every(k => v[k] === undefined || typeof v[k] === 'string' && v[k].length <= 128 && !/[\u0000-\u001f\u007f]/.test(v[k])) &&
    ['attempt', 'page'].every(k => v[k] === undefined || Number.isSafeInteger(v[k]) && Number(v[k]) > 0) &&
    (v.slot === undefined || v.slot === 0 || v.slot === 1)
}
