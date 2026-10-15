import { readFileSync } from 'node:fs'
import type { FailedAttempt } from '../src/components/updates/FailedAttempts.vue'
import type { LogDetail } from '../src/app/logDetail'
import type { RequestEvidence } from '../src/app/requestDiagnostics'

export const diagnosticScenarios = ['unfamiliar_transport', 'unfamiliar_HTTP', 'unknown_business', 'invalid_envelope', 'invalid_data', 'empty_HTTP', 'partial_body', 'initial_token_502', 'page_401_then_token_502'] as const
export function diagnosticCapture(name: typeof diagnosticScenarios[number]) {
  return JSON.parse(readFileSync(new URL('./diagnostics/' + name + '.json', import.meta.url), 'utf8')) as {
    provenance: string
    attempt: FailedAttempt & { diagnostics: RequestEvidence[] }
    log_detail: LogDetail & { diagnostics: RequestEvidence[] }
  }
}
