import { createHash } from 'node:crypto'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import assert from 'node:assert/strict'
const root = new URL('../../', import.meta.url)
const read = path => readFileSync(new URL(path, root))
const json = path => JSON.parse(read(path))
const hash = path => createHash('sha256').update(read(path)).digest('hex')
const manifestPath = 'frontend/fixtures/v1/manifest.json'
const requests = json('mock_data/requests.json')
const sources = Object.fromEntries(requests.filter(r => r.snapshot === 'initial' && r.mode === 'full' && r.params.page === 1).map(r => [r.table, {
  success: `mock_data/${r.response}`,
  empty: `mock_data/${requests.find(other => other.table === r.table && other.snapshot === 'initial' && other.mode === 'full' && other.expected_primary_keys.length === 0).response}`,
}]))
const files = new Set(['frontend/fixtures/v1/business.json', 'mock_data/manifest.json', 'mock_data/requests.json', 'mock_data/expected/scenarios.json', 'development_docs/verification/operations_http_examples.json', 'go.mod', 'go.sum'])
for (const [name, paths] of Object.entries(sources)) {
  files.add(`mock_data/snapshots/initial/${name}.json`)
  Object.values(paths).forEach(path => files.add(path))
}
for (const directory of ['internal/query', 'internal/operations', 'internal/testkit/querymock']) {
  for (const file of readdirSync(new URL(`${directory}/`, root), { recursive: true }).map(path => String(path).replaceAll('\\', '/'))) {
    if (file.endsWith('.go') && (!file.endsWith('_test.go') || ['handlers/pipeline_test.go', 'handlers/grades_test.go', 'handlers/frontend_fixture_export_test.go'].includes(file))) files.add(`${directory}/${file}`)
  }
}
for (const file of readdirSync(new URL('internal/query/contract/testdata/', root)).filter(file => file.endsWith('.json'))) files.add(`internal/query/contract/testdata/${file}`)
for (const name of ['empty_missing', 'empty_json_null', 'empty_empty_string', 'empty_literal_null', 'unknown_business_code']) {
  files.add(`mock_data/supplemental/${name}/input.json`)
  files.add(`mock_data/supplemental/${name}/expected.json`)
}
const expected = { version: 1, developmentOnly: true, sourceSnapshot: 'initial', sources, files: Object.fromEntries([...files].sort().map(path => [path, hash(path)])) }
const updating = process.argv.includes('--write')
if (!updating) assert.deepEqual(json(manifestPath), expected, 'Fixture provenance changed: re-export and review before explicitly updating the manifest')
assert.equal(Object.keys(sources).length, 14)
const sourceManifest = json('mock_data/manifest.json')
for (const path of files) {
  if (path.startsWith('mock_data/') && sourceManifest.files[path.slice(10)]) assert.equal(hash(path), sourceManifest.files[path.slice(10)], `Original mock changed: ${path}`)
}
const business = json('frontend/fixtures/v1/business.json')
const catalog = business.catalog
assert.equal(catalog.length, 23)
assert.equal(catalog.filter(api => api.kind === 'business').length, 9)
assert.equal(catalog.filter(api => api.kind === 'source').length, 14)
assert(catalog.every(api => api.readiness === 'unknown'))
const captures = business.captures
assert.equal(captures.length, 34)
for (const api of catalog.filter(api => api.kind === 'business')) {
  const cases = captures.filter(capture => capture.operation === api.id)
  assert.deepEqual(cases.map(c => c.scenario), api.snapshot ? ['success', 'empty', 'invalid_argument', 'snapshot_expired'] : ['success', 'empty', 'invalid_argument'])
  for (const capture of cases) {
    if (capture.scenario === 'success' || capture.scenario === 'empty') {
      assert.equal(capture.status, 200)
      assert.equal(capture.response.code, 'ok')
      assert.equal(capture.response.data.items.length > 0, capture.scenario === 'success')
      assert.equal(Boolean(capture.headers['X-Query-Snapshot']), api.snapshot)
    } else {
      assert.equal(capture.status, capture.scenario === 'invalid_argument' ? 400 : 409)
      assert.equal(capture.response.code, capture.scenario === 'invalid_argument' ? 'invalid_argument' : 'conflict')
    }
  }
}
const items = operation => captures.find(c => c.operation === operation && c.scenario === 'success').response.data.items
// These independent expected constants predate the exporter. Read, never eval.
for (const [file, constant, operation, identity] of [
  ['baseline', 'studentOne', 'students.query', 'student_number'],
  ['staff', 'staffOne', 'staff.query', 'staff_number'],
  ['organizations', 'organizationRoot', 'organizations.query', 'organization_code'],
  ['classes', 'sharedClass', 'staff.teaching-classes.query', 'teaching_class_number'],
  ['roster', 'firstRoster', 'teaching-classes.students.query', 'student_number'],
]) {
  const source = read(`internal/testkit/querymock/${file}.go`).toString('utf8')
  const match = source.match(new RegExp('const ' + constant + ' = `([\\s\\S]*?)`'))
  assert(match, `Missing independent expected constant ${constant}`)
  for (const expected of JSON.parse(match[1])) assert.deepEqual(items(operation).find(row => row[identity] === expected[identity]), expected)
}
assert.equal(items('staff.teaching-classes.query').find(row => row.teaching_class_number === 'J2026-0001').student_count, items('teaching-classes.students.query').length)
for (const paths of Object.values(sources)) {
  assert(json(paths.success).data.list.length > 0)
  assert.equal(json(paths.empty).data.list.length, 0)
}
assert(captures.some(c => JSON.stringify(c.response.data).includes(':null')), 'Missing actual null coverage')
if (updating) writeFileSync(new URL(manifestPath, root), JSON.stringify(expected, null, 2) + '\n')
console.log(`F00-04 PASS: 34 backend captures, 9 operations, 14 original source response pairs, independent expected rows and ${files.size} provenance hashes. No production I/O.`)
