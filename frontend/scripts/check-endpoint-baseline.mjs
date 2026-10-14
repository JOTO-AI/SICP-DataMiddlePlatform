import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const root = new URL('../../', import.meta.url)
const read = path => readFileSync(new URL(path, root), 'utf8')
const document = read('development_docs/FRONTEND_ENDPOINT_BASELINE.md')
const registered = new Set()
for (const file of ['routes', 'operations', 'api_catalog', 'notifications', 'maintenance', 'logs']) {
  for (const match of read(`internal/admin/${file}.go`).matchAll(/"(GET|POST|PUT) (\/(?:internal\/)?admin\/[^" ]+)"/g)) {
    registered.add(`${match[1]} ${match[2].replace('/internal/admin/', '/admin/')}`)
  }
}
for (const match of read('internal/application/application.go').matchAll(/"(GET|POST) (\/(?:console-api|admin)\/[^" ]+)"/g)) {
  registered.add(`${match[1]} ${match[2].replace('/{$}', '/')}`)
}
// The two health proxies allow exactly these four public read paths.
for (const service of ['query', 'worker']) for (const path of ['livez', 'readyz']) registered.add(`GET /console-api/${service}/${path}`)
const business = [...read('internal/query/contract/operations.go').matchAll(/"(\/api\/v1\/sicp\/[^" ]+)"/g)].map(match => `POST ${match[1]}`)
assert.equal(business.length, 9)
for (const endpoint of business) registered.add(endpoint)
const documented = new Set([...document.matchAll(/^\| (GET|POST|PUT) \| ([^ |]+) \|/gm)].map(match => `${match[1]} ${match[2]}`))
assert.deepEqual([...documented].sort(), [...registered].sort(), 'Documented endpoints must match backend registration')
const sources = [...read('internal/storage/source/catalog.go').matchAll(/Name: "([^"]+)", PrimaryKey: "([^"]+)"/g)]
assert.equal(sources.length, 14)
for (const [, name, key] of sources) assert(document.includes(`| ${name} | ${key} |`), `Missing source ${name}/${key}`)
const examples = JSON.parse(read('development_docs/verification/operations_http_examples.json'))
for (const example of examples.examples) assert(registered.has(example.pattern), `Unknown example route: ${example.pattern}`)
console.log(`F00-02 PASS: ${registered.size} method/path entries, 9 business operations, 14 source IDs and existing HTTP examples covered. Runtime acceptance not executed.`)
