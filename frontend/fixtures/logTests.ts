import { managementCapture } from './backend'
// Existing backend DTO example projected to its EventItem base fields (dto.go).
// Only query metadata is adapted to exercise the protocol; no new log rows.
export function recordedLogPage(source: 'platform' | 'upstream' = 'platform') {
  const envelope = managementCapture('log_history').response as { code: string; data: any }
  envelope.data.source = source
  if (source === 'platform') {
    const detail = managementCapture('log_detail').response.data as Record<string, unknown>
    const { entity_id, metadata, response, redaction, ...item } = detail
    envelope.data.items = [item]
    envelope.data.high_water = item.sequence
    envelope.data.filter.to = '2026-10-14T00:00:00Z'
  }
  return envelope
}
