import { managementCapture, sourceResponse } from './backend'
import { objectTokens } from '../src/shared/jsonObject'
import type { SourceTestData } from '../src/app/sourceTest'
// Job lifecycle metadata is a protocol boundary. All normal source records and numeric
// tokens come unchanged from the existing generated mock file, never handwritten rows.
export function sourceJobResult(table: string, status = 'succeeded', scenario: 'success' | 'empty' = 'success', available = status === 'succeeded') {
  const capture = managementCapture('test_result')
  const data = capture.response.data as SourceTestData
  data.job.status = status
  data.result_state = available ? 'available' : 'result_unavailable'
  const tokens = objectTokens(objectTokens(sourceResponse(table, scenario)).get('data')!)
  const resultText = available ? `{"total":${tokens.get('total')},"list":${tokens.get('list')}}` : 'null'
  const raw = `{"code":"ok","message":"","data":{"job":${JSON.stringify(data.job)},"result_state":${JSON.stringify(data.result_state)},"result":${resultText}}}`
  return { path: capture.path, status: capture.status, raw, resultText }
}
