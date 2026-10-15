// Backend nanoseconds may be decimal strings larger than Number.MAX_SAFE_INTEGER.
// Decimal shifting preserves every digit and keeps zero separate from missing values.
export function milliseconds(value: unknown, empty = '未记录'): string {
  if (value == null) return empty
  if (typeof value === 'number' && (!Number.isSafeInteger(value) || value < 0)) return '数值无法精确显示'
  if (typeof value !== 'number' && typeof value !== 'string') return '无效耗时'
  const text = String(value)
  if (!/^\d+$/.test(text) || text.length > 128) return '无效耗时'
  const padded = text.replace(/^0+(?=\d)/, '').padStart(7, '0')
  const fraction = padded.slice(-6).replace(/0+$/, '')
  return padded.slice(0, -6) + (fraction ? '.' + fraction : '')
}
