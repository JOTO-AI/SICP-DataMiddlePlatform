import { objectTokens } from '../shared/jsonObject'
import { recordCell } from './responseViews'
export function personFields(row: Map<string, string>) {
  const fields: { path: string; key: string; value: string }[] = []
  let truncated = false
  function visit(tokens: Map<string, string>, parent = '', depth = 0) {
    for (const [key, raw] of tokens) {
      if (fields.length >= 200) { truncated = true; break }
      const path = parent ? `${parent}.${key}` : key
      if (raw.startsWith('{') && objectTokens(raw).size && depth < 6) visit(objectTokens(raw), path, depth + 1)
      else fields.push({ path, key, value: recordCell(new Map([[key, raw]]), key) })
    }
  }
  visit(row)
  return { fields, truncated }
}