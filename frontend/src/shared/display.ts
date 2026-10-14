export function displayValue(value: unknown): string {
  if (value == null || value === '') return '未提供'
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (Array.isArray(value)) return value.length ? value.map(displayValue).join('、') : '无'
  if (typeof value === 'object') return '对象（请查看详情）'
  return String(value)
}
export function displayEnum(value: string | null | undefined, labels: Record<string, string>): string {
  if (value == null || value === '') return '未提供'
  return Object.hasOwn(labels, value) ? labels[value]! : `未知状态（${value}）`
}
export function validTimestamp(value?: string | null): value is string {
  return !!value && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && !Number.isNaN(Date.parse(value))
}
export function displayTimestamp(value?: string | null, empty = '未提供'): string {
  if (!value) return empty
  if (!validTimestamp(value)) return '未知时间'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleString('zh-CN', { hour12: false })
}
export function fieldValue(row: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => value && typeof value === 'object' && Object.hasOwn(value, key) ? (value as Record<string, unknown>)[key] : undefined, row)
}
