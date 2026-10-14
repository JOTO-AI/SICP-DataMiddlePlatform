import { readFileSync } from 'node:fs'

type Capture = {
  operation: string
  scenario: 'success' | 'empty' | 'invalid_argument' | 'snapshot_expired'
  request: Record<string, unknown>
  request_snapshot?: string
  status: number
  headers: Record<string, string>
  response: { code: string; message: string; request_id?: string; data: unknown }
}
const fixtureRoot = new URL('./v1/', import.meta.url)
const document = JSON.parse(readFileSync(new URL('business.json', fixtureRoot), 'utf8')) as { captures: Capture[]; catalog: unknown[] }
const manifest = JSON.parse(readFileSync(new URL('manifest.json', fixtureRoot), 'utf8')) as { sources: Record<string, { success: string; empty: string }> }

// Only test code may import this module. Every call returns an independent value.
export function backendCapture(operation: string, scenario: Capture['scenario'] = 'success'): Capture {
  const capture = document.captures.find(item => item.operation === operation && item.scenario === scenario)
  if (!capture) throw new Error(`No recorded backend fixture: ${operation}/${scenario}`)
  return structuredClone(capture)
}
export function backendCatalog(): unknown[] { return structuredClone(document.catalog) }

// Raw text preserves upstream JSON numeric tokens, including integers beyond 2^53.
export function sourceResponse(table: string, scenario: 'success' | 'empty' = 'success'): string {
  const path = manifest.sources[table]?.[scenario]
  if (!path) throw new Error(`No recorded source fixture: ${table}/${scenario}`)
  return readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8')
}

// Existing backend management examples; selected-field samples keep omissions as unknown.
const management = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')) as { examples: { name: string; path: string; status: number; response: { code: string; message?: string; data: unknown } }[] }
export function managementCapture(name: string) {
  const example = management.examples.find(item => item.name === name)
  if (!example) throw new Error(`No recorded management example: ${name}`)
  return structuredClone(example)
}
