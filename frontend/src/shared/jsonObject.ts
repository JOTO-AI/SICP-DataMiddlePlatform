// Read top-level raw values only after JSON.parse has confirmed JSON syntax.
// Keeping tokens prevents duplicate keys and integer exponent/decimal spellings being normalized away.
export function objectTokens(text: string): Map<string, string> {
  let i = text.indexOf('{') + 1
  const values = new Map<string, string>()
  const whitespace = () => { while (/\s/.test(text[i] ?? '') && i < text.length) i++ }
  function stringEnd() {
    i++
    while (i < text.length) { if (text[i] === '\\') i += 2; else if (text[i++] === '"') return }
  }
  whitespace()
  while (i < text.length && text[i] !== '}') {
    const keyStart = i; stringEnd()
    const key = JSON.parse(text.slice(keyStart, i)) as string
    if (values.has(key)) throw new Error(`${key}：字段重复，原文已保留。`)
    whitespace(); i++; whitespace()
    const start = i
    let depth = 0
    while (i < text.length) {
      const char = text[i]
      if (char === '"') { stringEnd(); continue }
      if ((char === ',' || char === '}') && depth === 0) break
      if (char === '[' || char === '{') depth++
      else if (char === ']' || char === '}') depth--
      i++
    }
    values.set(key, text.slice(start, i).trim())
    if (text[i] === ',') i++
    whitespace()
  }
  return values
}

// Split a validated JSON array without converting number tokens. Extra rows remain in the raw response.
export function arrayTokens(text: string, limit = 1000): { values: string[]; truncated: boolean } {
  let i = text.indexOf('[') + 1, start = i, depth = 0, quoted = false
  const values: string[] = []
  while (i < text.length) {
    const char = text[i]!
    if (quoted) { if (char === '\\') i++; else if (char === '"') quoted = false }
    else if (char === '"') quoted = true
    else if (depth === 0 && (char === ',' || char === ']')) {
      const value = text.slice(start, i).trim()
      if (value) { if (values.length === limit) return { values, truncated: true }; values.push(value) }
      if (char === ']') return { values, truncated: false }
      start = i + 1
    } else if (char === '[' || char === '{') depth++
    else if (char === ']' || char === '}') depth--
    i++
  }
  throw new Error('记录数组不完整。')
}
