import type { CatalogAPI } from './catalog'
import { operations } from '../queryOperations'
import { fieldDescription } from './catalogSchema'
import type { ParameterDefinition } from '../shared/json'
import { objectTokens } from '../shared/jsonObject'
const bytes = (value: string) => new TextEncoder().encode(value).length
const goWhitespace = /^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$/
function validString(value: string) {
  return bytes(value) <= 512 && !goWhitespace.test(value) && ![...value].some(char => {
    const code = char.codePointAt(0)!
    return code < 32 || code === 127 || code === 0xfffd || code >= 0xd800 && code <= 0xdfff
  })
}
export function testParameters(api: CatalogAPI): ParameterDefinition[] {
  const common = new Set([...(operations.find(operation => operation.operation === api.id)?.fields.map(field => field.key) ?? []), 'page', 'page_size'])
  return Object.entries(api.request.properties ?? {}).sort(([, a], [, b]) => Number(b.required) - Number(a.required)).map(([name, field]) => ({ name, type: field.type, itemType: field.items?.type, required: field.required,
    nullable: !field.required, description: fieldDescription(api, name), advanced: !field.required && !common.has(name), defaultText: field.default == null ? undefined : JSON.stringify(field.default) }))
}
export function editableExample(api: CatalogAPI, text: string): string {
  if (api.kind !== 'source') return text
  try {
    const body = JSON.parse(text)
    if (!body || typeof body !== 'object' || Array.isArray(body)) return text
    // Catalog source examples include fixed job context, which is displayed outside editable parameters.
    if (body.api_id !== undefined && body.api_id !== api.id || body.version !== undefined && body.version !== api.version) return text
    delete body.api_id; delete body.version; delete body.idempotency_key
    return JSON.stringify(body, null, 2)
  } catch { return text }
}
export function validateTestRequest(api: CatalogAPI, text: string): Record<string, unknown> {
  if (bytes(text) > 65536) throw new Error('请求正文超过 65536 字节（含空白）。')
  const body: unknown = JSON.parse(text)
  if (!body || typeof body !== 'object' || Array.isArray(body)) throw new Error('请求须为单个 JSON 对象。')
  const values = body as Record<string, unknown>, tokens = objectTokens(text), fields = api.request.properties ?? {}
  const issue = (name: string, message: string): never => { throw new Error(`${name}：${message}`) }
  for (const [name, field] of Object.entries(fields)) if (field.required && (!Object.hasOwn(values, name) || values[name] === null)) issue(name, '此参数必填且不能为 null。')
  for (const [name, value] of Object.entries(values)) {
    if (!Object.hasOwn(fields, name)) issue(name, '当前接口不接受此字段。')
    const field = fields[name]!
    if (value === null && !field.required) continue
    switch (field.type) {
      case 'string':
        if (typeof value !== 'string' || !validString(value)) issue(name, '须为非空字符串，最多 512 UTF-8 字节，不能包含控制字符或无效 Unicode。')
        if (name === 'semester' && value !== '1' && value !== '2') issue(name, '学期仅接受字符串 1 或 2。')
        if (name === 'academic_year') {
          if (value === 'all' && api.id.includes('grades.')) break
          const match = /^(\d{4})-(\d{4})$/.exec(value as string)
          if (!match || Number(match[1]) < 1000 || Number(match[2]) !== Number(match[1]) + 1) issue(name, '须为相邻四位年份 YYYY-YYYY；仅成绩接口允许 all。')
        }
        if (['admission_year', 'expected_graduation_year'].includes(name) && (!/^\d{4}$/.test(value as string) || value === '0000')) issue(name, '须为 0001–9999 的四位年份字符串。')
        break
      case 'array':
        if (!Array.isArray(value) || value.length < 1 || value.length > 1000 || value.some(item => typeof item !== 'string' || !validString(item))) issue(name, '须为 1–1000 个非空字符串组成的数组。')
        break
      case 'integer': {
        if (typeof value !== 'number' || !Number.isSafeInteger(value) || !/^-?(0|[1-9]\d*)$/.test(tokens.get(name) ?? '')) issue(name, '须为整数 JSON 数字，不接受字符串、小数或指数形式。')
        const min = api.kind === 'source' ? 0 : 1
        const max = name === 'page' ? 1000000 : name === 'study_years' ? 2147483647 : api.kind === 'source' ? 100 : 1000
        if ((value as number) < min || (value as number) > max) issue(name, `须在 ${min}–${max} 范围内。`)
        break
      }
      case 'number': if (typeof value !== 'number' || !Number.isFinite(value)) issue(name, '须为有限 JSON 数字。'); break
      case 'boolean': if (typeof value !== 'boolean') issue(name, '仅接受 true、false 或省略。'); break
      default: issue(name, '当前编辑器尚不支持此目录字段类型，请核对接口版本。')
    }
  }
  if (typeof values.score_min === 'number' && typeof values.score_max === 'number' && values.score_min > values.score_max) issue('score_min', '不能大于 score_max；成绩范围没有固定 0–100 限制。')
  return values
}
