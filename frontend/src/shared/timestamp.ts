// Preserve PostgreSQL microseconds; Date.parse alone discards the last 3 digits.
export function timestampMicros(value: unknown): bigint | null {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,6})?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$/.test(value)) return null
  const millis = Date.parse(value)
  if (!Number.isFinite(millis)) return null
  const fraction = value.match(/\.(\d+)/)?.[1] ?? ''
  return BigInt(millis) * 1000n + BigInt(fraction.padEnd(6, '0').slice(3))
}