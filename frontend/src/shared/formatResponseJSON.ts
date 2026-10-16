// Validate JSON syntax, then format its original tokens. Never serialize parsed
// numbers: large integers, decimals, exponents, duplicate keys and escapes must
// remain exactly as returned by the backend.
export function formatResponseJSON(text: string): string | null {
  if (text.length > 2 * 1024 * 1024) return null
  try { JSON.parse(text) } catch { return null }
  const chunks: string[] = []
  let i = 0, depth = 0, length = 0, previous = ''
  const whitespace = () => { while (i < text.length && /[\t\r\n ]/.test(text[i]!)) i++ }
  const push = (part: string) => { chunks.push(part); length += part.length }
  const line = () => push('\n' + '  '.repeat(depth))
  while (i < text.length) {
    whitespace()
    if (i === text.length) break
    const char = text[i++]!
    if (char === '{' || char === '[') {
      push(char); depth++
      if (depth > 64) return null
      whitespace()
      if (text[i] !== '}' && text[i] !== ']') line()
    } else if (char === '}' || char === ']') {
      depth--
      if (previous !== '{' && previous !== '[') line()
      push(char)
    } else if (char === ',') { push(char); line() }
    else if (char === ':') push(': ')
    else {
      const start = i - 1
      if (char === '"') {
        while (i < text.length) {
          if (text[i] === '\\') i += 2
          else if (text[i++] === '"') break
        }
      } else {
        while (i < text.length && !/[\t\r\n ,}\]]/.test(text[i]!)) i++
      }
      push(text.slice(start, i))
    }
    if (length > 4 * 1024 * 1024) return null
    previous = char
  }
  return chunks.join('')
}
