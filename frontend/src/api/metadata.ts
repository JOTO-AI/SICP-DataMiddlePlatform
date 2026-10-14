export const responseHeaderNames = [
  'Content-Type', 'Cache-Control', 'X-Request-ID', 'X-Query-Snapshot',
  'X-SICP-Combination', 'Location', 'Retry-After',
] as const
export type ResponseHeaders = Readonly<Partial<Record<typeof responseHeaderNames[number], string>>>

export function responseMetadata(headers: Headers): ResponseHeaders {
  const result: Partial<Record<typeof responseHeaderNames[number], string>> = {}
  for (const name of responseHeaderNames) {
    const value = headers.get(name)
    if (value !== null) result[name] = value
  }
  return Object.freeze(result)
}
