// Extract documentation labels only, never samples or credentials.
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
const docs = new URL('../../latest_api_docs/', import.meta.url)
const labels = {}
for (const file of readdirSync(docs).sort()) {
  if (!file.endsWith('.md') || file === '源接口文档汇总.md') continue
  const text = readFileSync(new URL(file, docs), 'utf8')
  const id = text.match(/请求路径：`\/(dwd_[a-z]+)`/)?.[1]
  if (!id) continue
  const fields = {}
  const section = text.split('### 5.2 ')[1]?.split(/\n## /)[0] ?? ''
  for (const line of section.split('\n')) {
    const cells = line.split('|').map(cell => cell.trim())
    const key = cells[1]?.match(/^`([A-Z0-9_]+)`$/)?.[1]
    if (!key || !cells[5]) continue
    fields[key] = cells[5].split(/[；。<]/)[0]
  }
  labels[id] = fields
}
if (Object.keys(labels).length !== 14 || Object.values(labels).some(fields => !Object.keys(fields).length)) throw new Error('Expected labels for all 14 source documents')
writeFileSync(new URL('../src/app/sourceFieldLabels.ts', import.meta.url), '// Generated from latest_api_docs response field descriptions; regenerate with scripts/generate-source-field-labels.mjs.\nexport const sourceFieldLabels: Record<string, Record<string, string>> = ' + JSON.stringify(labels, null, 2) + '\n')
