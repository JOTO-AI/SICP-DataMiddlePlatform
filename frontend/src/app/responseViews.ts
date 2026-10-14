import { arrayTokens, objectTokens } from '../shared/jsonObject'
import { operations } from '../queryOperations'
import type { CatalogAPI } from './catalog'
import { fieldDescription } from './catalogSchema'
import { gradeColumns } from './grades'
export interface ResponseRecords { rows: Map<string, string>[]; objects: Record<string, unknown>[]; truncated: boolean; error: string }
export function responseRecords(raw: string, kind: string): ResponseRecords {
  const empty = { rows: [], objects: [], truncated: false }
  try {
    const body = JSON.parse(raw)
    let text = objectTokens(raw).get('data'), data = body.data
    if (kind === 'source') {
      if (data?.job?.status !== 'succeeded' || data?.result_state !== 'available') return { ...empty, error: '当前作业尚无可用的成功结果。' }
      text = objectTokens(text!).get('result'); data = data.result
    }
    const key = kind === 'source' ? 'list' : 'items'
    if (!data || !Array.isArray(data[key])) return { ...empty, error: '响应没有合法记录数组，请查看 JSON 原文。' }
    const list = arrayTokens(objectTokens(text!).get(key)!, 1000)
    const objects = list.values.map(value => JSON.parse(value) as unknown)
    if (objects.some(value => !value || typeof value !== 'object' || Array.isArray(value))) return { ...empty, error: '记录结构异常，请查看 JSON 原文。' }
    return { rows: list.values.map(objectTokens), objects: objects as Record<string, unknown>[], truncated: list.truncated, error: '' }
  } catch { return { ...empty, error: '响应无法转换为记录视图；原文已保留。' } }
}
export function rawField(row: Map<string, string>, path: string): string | undefined {
  const keys = path.split('.'); let raw = row.get(keys.shift()!)
  try { for (const key of keys) { if (!raw?.startsWith('{')) return undefined; raw = objectTokens(raw).get(key) } } catch { return undefined }
  return raw
}
export function recordCell(row: Map<string, string>, path: string): string {
  const raw = rawField(row, path)
  if (raw === undefined) return '未返回'
  if (raw === 'null') return 'null（未提供）'
  if (raw === '""') return '空字符串'
  const value = raw.startsWith('"') ? JSON.parse(raw) as string : raw
  let end = Math.min(256, value.length)
  if (end < value.length && /[\uD800-\uDBFF]/.test(value[end - 1] ?? '')) end--
  return value.slice(0, end) + (end < value.length ? '…（见 JSON）' : '')
}
export function responseColumns(api: CatalogAPI, records: ResponseRecords) {
  const operation = operations.find(op => op.operation === api.id)
  if (operation?.kind === 'grades') return gradeColumns
  const paths = operation?.kind === 'timetable' ? ['course_code', 'course_name', 'teaching_class_number', 'teacher_names', 'teacher_staff_numbers', 'dates', 'weeks', 'sections', 'location']
    : operation?.columns?.map(column => column.key) ?? [...new Set(records.rows.flatMap(row => [...row.keys()]))].slice(0, 24)
  return paths.map(key => ({ key, title: operation?.columns?.find(column => column.key === key)?.title ?? fieldDescription(api, key.split('.').at(-1)!) }))
}
export interface OrganizationNode { index: number; depth: number; note: string }
export function organizationNodes(rows: Record<string, unknown>[]): OrganizationNode[] {
  const byID = new Map<string, number[]>(), children = new Map<number, number[]>(), roots: { index: number; note: string }[] = []
  rows.forEach((row, i) => { if (typeof row.organization_code === 'string') byID.set(row.organization_code, [...(byID.get(row.organization_code) ?? []), i]) })
  rows.forEach((row, index) => {
    const parent = row.parent_organization_code, matches = typeof parent === 'string' ? byID.get(parent) : undefined
    if (matches?.length === 1 && matches[0] !== index) children.set(matches[0]!, [...(children.get(matches[0]!) ?? []), index])
    else roots.push({ index, note: parent === null ? '上级为 null' : matches?.length ? '上级关系有歧义' : '上级未在本次响应中' })
  })
  const result: OrganizationNode[] = [], visited = new Set<number>()
  function visit(index: number, note = '') {
    const stack = [{ index, depth: 0, note }]
    while (stack.length) {
      const node = stack.pop()!
      if (visited.has(node.index)) continue
      visited.add(node.index); result.push(node)
      for (const child of [...(children.get(node.index) ?? [])].reverse()) stack.push({ index: child, depth: node.depth + 1, note: '' })
    }
  }
  for (const root of roots) visit(root.index, root.note)
  rows.forEach((_, index) => { if (!visited.has(index)) visit(index, '关系闭环，按本次记录展示') })
  return result
}
export function datedLessons(rows: Record<string, unknown>[]) {
  const groups = new Map<string, number[]>(), unmapped: number[] = []
  let clipped = false, mappings = 0
  rows.forEach((row, index) => {
    const dates = Array.isArray(row.dates) ? row.dates.filter((date): date is string => typeof date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(date) && Number.isFinite(Date.parse(date)) && new Date(date).toISOString().startsWith(date)) : []
    if (!dates.length || !Array.isArray(row.sections) || !row.sections.length || !row.sections.every(section => Number.isSafeInteger(section) && Number(section) > 0)) { unmapped.push(index); return }
    for (const date of new Set(dates)) { if (mappings >= 2000) { clipped = true; break }; groups.set(date, [...(groups.get(date) ?? []), index]); mappings++ }
  })
  return { groups: [...groups.entries()].sort(([a], [b]) => a.localeCompare(b)), unmapped, clipped }
}
