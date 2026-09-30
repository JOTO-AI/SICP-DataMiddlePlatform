// getRandomValues also works on plain HTTP intranet origins, where randomUUID
// is unavailable. The API accepts an opaque key, so 128 random bits suffice.
export function newIdempotencyKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}
