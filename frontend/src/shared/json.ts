import { objectTokens } from './jsonObject'
export interface ParameterDefinition { name: string; type: string; itemType?: string; required?: boolean; nullable?: boolean; description?: string; defaultText?: string; advanced?: boolean }
export const MAX_EDITOR_CHARACTERS = 262144
export const MAX_EDITOR_FIELDS = 100
export function parseEditableJSON(text: string): unknown {
  if (text.length > MAX_EDITOR_CHARACTERS) throw new Error('请求文本超过 262144 字符，请缩小内容后编辑。')
  return JSON.parse(text, (_key, value: unknown) => {
    if (typeof value === 'number' && (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value)))) throw new Error('包含无法精确转换的数字，请保留 JSON 原文；字符串编号请使用引号。')
    return value
  })
}
export function parseRequestObject(text: string): Record<string, unknown> {
  const value = parseEditableJSON(text)
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('请求须为 JSON 对象。')
  if (Object.keys(value).length > MAX_EDITOR_FIELDS) throw new Error('参数超过 100 项，请保留 JSON 原文。')
  objectTokens(text)
  return value as Record<string, unknown>
}
export function jsonError(error: unknown): string { return error instanceof SyntaxError ? 'JSON 格式错误，请检查引号、逗号与括号；原文已保留。' : error instanceof Error ? error.message : '无法转换，原文已保留。' }
export function parameterError(value: unknown, definition: ParameterDefinition): string {
  if (value === null) return definition.nullable ? '' : '不允许 null'
  const type = Array.isArray(value) ? 'array' : typeof value
  const valid = definition.type === 'integer' ? typeof value === 'number' && Number.isSafeInteger(value) : type === definition.type
  return valid ? '' : `应为 ${definition.type}`
}
export function boundedText(text: string, expanded: boolean): { text: string; truncated: boolean; limit: number } {
  const limit = expanded ? 65536 : 4096
  let end = Math.min(text.length, limit)
  if (end < text.length && /[\uD800-\uDBFF]/.test(text[end - 1] ?? '')) end--
  return { text: text.slice(0, end), truncated: end < text.length, limit }
}
