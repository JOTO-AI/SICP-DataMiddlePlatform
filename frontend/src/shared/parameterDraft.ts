import { parseEditableJSON, type ParameterDefinition } from './json'

export interface ParameterDraft {
  name: string; text: string; error: string
  format: 'text' | 'lines' | 'boolean' | 'json'
  present: boolean; original: unknown; changed: boolean
}
export function parameterDraft(name: string, body: Record<string, unknown>, definition?: ParameterDefinition): ParameterDraft {
  const present = Object.hasOwn(body, name), value = body[name]
  let format: ParameterDraft['format'] = definition?.type === 'string' ? 'text' :
    definition?.type === 'boolean' ? 'boolean' : definition?.type === 'array' && definition.itemType === 'string' ? 'lines' : 'json'
  // Invalid examples must remain invalid when a different field is edited.
  if (present && value !== null && (format === 'text' && typeof value !== 'string' ||
    format === 'boolean' && typeof value !== 'boolean' ||
    format === 'lines' && (!Array.isArray(value) || value.some(v => typeof v !== 'string' || /[\r\n]/.test(v))))) format = 'json'
  const text = !present ? '' : format === 'text' ? value === null ? '' : value as string :
    format === 'lines' ? value === null ? '' : (value as string[]).join('\n') : JSON.stringify(value, null, 2)
  return { name, text, format, present, original: value, changed: false, error: '' }
}
// Undefined means omitted. Unedited nulls, empty strings and invalid examples
// retain their original types; typing in a text field never parses JSON.
export function parameterDraftValue(row: ParameterDraft): unknown {
  if (!row.changed) return row.present ? row.original : undefined
  if (row.text === '' || row.format === 'json' && !row.text.trim()) return undefined
  if (row.format === 'text') return row.text
  if (row.format === 'lines') return row.text.split(/\r?\n/)
  return parseEditableJSON(row.text)
}
