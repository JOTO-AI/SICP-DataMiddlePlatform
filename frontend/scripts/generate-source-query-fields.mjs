// Query metadata from provider documentation, excluding examples and credentials.
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
const docs = new URL('../../latest_api_docs/', import.meta.url)
const queries = {}
for (const file of readdirSync(docs).sort()) {
  if (!file.endsWith('.md') || file === '源接口文档汇总.md') continue
  const text = readFileSync(new URL(file, docs), 'utf8')
  const id = text.match(/请求路径：`\/(dwd_[a-z]+)`/)?.[1]
  if (!id) continue
  const section = text.split('## 3. Query 参数')[1]?.split('## 4. ')[0] ?? ''
  const fields = []
  for (const line of section.split('\n')) {
    const cells = line.split('|').map(cell => cell.trim())
    const name = cells[1]?.match(/^`([A-Za-z0-9_]+)`$/)?.[1]
    if (!name || !cells[6]) continue
    fields.push({ name, type: cells[2], required: cells[3], defaultText: cells[4], description: name === 'access_token' ? '由服务端部署配置；浏览器不读取或编辑令牌。' : cells[6] })
  }
  queries[id] = fields
}
if (Object.keys(queries).length !== 14 || Object.values(queries).some(fields => fields.length < 3)) throw new Error('Expected Query fields for all 14 sources')
writeFileSync(new URL('../src/app/sourceQueryFields.ts', import.meta.url), '// Generated from latest_api_docs section 3; regenerate with scripts/generate-source-query-fields.mjs.\nexport interface SourceQueryField { name: string; type: string; required: string; defaultText: string; description: string }\nexport const sourceQueryFields: Record<string, SourceQueryField[]> = ' + JSON.stringify(queries, null, 2) + '\n')
