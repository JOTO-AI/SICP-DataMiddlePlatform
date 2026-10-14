import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync, readdirSync } from 'node:fs'

// Resolve from the script so this check also works outside the repository root.
const root = new URL('../../', import.meta.url)
const read = path => readFileSync(new URL(path, root))
const plan = read('development_docs/FRONTEND_IMPLEMENTATION_PLAN.md').toString('utf8')
const matrix = read('development_docs/FRONTEND_VISUAL_BASELINE.md').toString('utf8')
const gallery = 'development_docs/ui-review-v1/'
const manifest = JSON.parse(read(`${gallery}manifest.json`))
const rows = matrix.split(/\r?\n/).filter(line => /^\| \d{2}-.+\.png \|/.test(line))
const images = readdirSync(new URL(gallery, root)).filter(name => name.endsWith('.png')).sort()
assert.equal(manifest.length, 28, 'Expected the 28-image V5 baseline')
assert.equal(rows.length, 28, 'Every image must have one traceability row')
assert.deepEqual(manifest.map(item => item.file).sort(), images, 'Gallery and manifest differ')
const seen = new Set()
const requirements = new Set()
for (const row of rows) {
  const cells = row.split('|').slice(1, -1).map(cell => cell.trim())
  assert.equal(cells.length, 6, 'Expected six traceability columns')
  const [file, requirement, implementation, tests, layout, correction] = cells
  assert(!seen.has(file), `Duplicate image: ${file}`)
  seen.add(file)
  const entry = manifest.find(item => item.file === file)
  assert(entry, `Image missing from manifest: ${file}`)
  assert.equal(entry.design_baseline, 'V5', `Wrong design version: ${file}`)
  const bytes = read(`${gallery}${file}`)
  assert.equal(bytes.length, entry.bytes, `Image size changed: ${file}`)
  assert.equal(createHash('sha256').update(bytes).digest('hex'), entry.sha256, `Image hash changed: ${file}`)
  assert.equal(bytes.subarray(0, 8).toString('hex'), '89504e470d0a1a0a', `Not a PNG: ${file}`)
  assert.equal(bytes.readUInt32BE(16), entry.width, `Width changed: ${file}`)
  assert.equal(bytes.readUInt32BE(20), entry.height, `Height changed: ${file}`)
  for (const [cell, pattern] of [[requirement, /^R\d{2}$/], [implementation, /^F\d{2}$/], [tests, /^FT\d{2}-\d{2}$/]]) {
    assert(cell, `Missing traceability: ${file}`)
    for (const id of cell.split(/\s+/)) {
      assert(pattern.test(id), `Malformed ID ${id}: ${file}`)
      assert(new RegExp(`\\b${id}\\b`).test(plan), `Unknown plan ID ${id}: ${file}`)
    }
  }
  requirement.split(/\s+/).forEach(id => requirements.add(id))
  assert(layout && correction, `Missing layout or contract correction: ${file}`)
}
assert.deepEqual([...seen].sort(), images, 'Traceability does not cover the gallery')
assert.deepEqual([...requirements].sort(), Array.from({ length: 11 }, (_, i) => `R${String(i + 1).padStart(2, '0')}`))
console.log('F00-01 PASS: 28 V5 images verified; R01-R11 and plan references covered. UI acceptance remains pending.')
