// Event IDs are lower-case 64-digit hex correlation IDs in logquery.Detail.
// Never hash, decode, or substitute an originating request/task ID here.
export function logDetailPath(source: string, eventId: string): string | null {
  if (!['platform', 'upstream'].includes(source) || !/^[a-f0-9]{64}$/.test(eventId)) return null
  return `/logs/${source}/events/${eventId}`
}
