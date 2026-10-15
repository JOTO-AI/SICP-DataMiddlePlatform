import { readdirSync, readFileSync, statSync, mkdirSync, writeFileSync } from 'node:fs'
import { resolve, relative, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..'), dist = resolve(root, 'dist')
function files(dir) { return readdirSync(dir).flatMap(name => { const p = resolve(dir, name); return statSync(p).isDirectory() ? files(p) : [p] }) }
const failures = [], assets = files(dist).map(path => {
  const data = readFileSync(path), text = data.toString('utf8'), name = relative(dist, path).replaceAll('\\', '/')
  if (!/\.(js|css|html)$/.test(name)) failures.push(`Unexpected artifact: ${name}`)
  if (/mockServiceWorker|msw\/browser|backendCapture|managementCapture|operations_http_examples|fixtures\/v1|BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY|QUERY_API_KEY|ADMIN_PASSWORD|SMTP_PASSWORD|SICP_WORKER_TARGET/.test(text.replaceAll('<QUERY_API_KEY>', 'DOCUMENTED_PLACEHOLDER'))) failures.push(`Forbidden bundle marker: ${name}`)
  if (/@import\s*(?:url\()?\s*["']?https?:|url\(["']?(?:https?:)?\/\//i.test(text) || /<(?:script|link)[^>]+(?:src|href)=["'](?:https?:)?\/\//i.test(text)) failures.push(`External resource: ${name}`)
  return { file: name, bytes: data.length, gzipBytes: gzipSync(data).length, sha256: createHash('sha256').update(data).digest('hex') }
})
const js = assets.filter(x => x.file.endsWith('.js'))
if (js.reduce((n, x) => n + x.bytes, 0) > 1024 * 1024) failures.push('JS total exceeds 1 MiB')
if (js.some(x => x.gzipBytes > 128 * 1024)) failures.push('JS chunk exceeds 128 KiB gzip')
for (const page of ['Overview', 'ApiCenter', 'ApiDetail', 'Tasks', 'Sources', 'Logs', 'Notifications', 'Settings']) if (!js.some(x => x.file.startsWith(`assets/${page}-`))) failures.push(`Missing lazy page: ${page}`)
const html = readFileSync(resolve(dist, 'index.html'), 'utf8')
for (const match of html.matchAll(/(?:src|href)="(\/console\/[^"?#]+)"/g)) if (!assets.some(x => '/console/' + x.file === match[1])) failures.push(`Missing index asset: ${match[1]}`)
for (const path of files(resolve(root, 'src'))) if (/from\s*['"][^'"]*(?:fixtures|e2e|test-results)\//.test(readFileSync(path, 'utf8'))) failures.push(`Test import in production source: ${relative(root, path)}`)
const git = (...args) => execFileSync('git', ['-c', `safe.directory=${resolve(root, '..').replaceAll('\\', '/')}`, ...args], { cwd: root, encoding: 'utf8' }).trim()
const sourceTreeSha256 = createHash('sha256').update([...files(resolve(root, 'src')), ...['vite.config.ts', 'package.json', 'package-lock.json'].map(p => resolve(root, p))].sort().map(p => relative(root, p).replaceAll('\\', '/') + ':' + createHash('sha256').update(readFileSync(p)).digest('hex')).join('\n')).digest('hex')
const output = resolve(root, '../artifacts/frontend-f12-04/build-audit.json'); mkdirSync(dirname(output), { recursive: true })
writeFileSync(output, JSON.stringify({ generatedAt: new Date().toISOString(), head: git('rev-parse', 'HEAD'), sourceTreeSha256, sourceChanges: git('status', '--porcelain', '--', 'src', 'vite.config.ts', 'package.json', 'package-lock.json'), assets, failures, scope: 'Static markers/import/resource/size audit; does not read .env or claim exhaustive secret detection.' }, null, 2))
console.log(JSON.stringify({ files: assets.length, jsBytes: js.reduce((n, x) => n + x.bytes, 0), failures, output }, null, 2))
if (failures.length) process.exitCode = 1