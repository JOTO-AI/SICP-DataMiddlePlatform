import { test, expect, type Page, type TestInfo } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { backendCatalog, managementCapture, paginationCapture } from '../fixtures/backend'
import { recordedLogPage } from '../fixtures/logTests'
test.beforeAll(async ({}, info) => { expect(info.config.workers, 'Capacity measurements require the registered single-worker environment').toBe(1) })
const mib = 1024 * 1024
async function setup(page: Page) {
  await page.route('**/admin/**', route => route.abort())
  await page.route('**/console-api/**', route => route.abort())
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  await page.route('**/console-api/logout', route => route.fulfill({ json: { code: 'ok', data: {} } }))
  const items = backendCatalog() as { id: string }[]
  await page.route('**/admin/v1/apis/*', route => route.fulfill({ json: { code: 'ok', data: items.find(item => item.id === new URL(route.request().url()).pathname.split('/').at(-1)) } }))
}
async function memory(page: Page) {
  const cdp = await page.context().newCDPSession(page)
  try { await cdp.send('HeapProfiler.collectGarbage'); const result = await cdp.send('Runtime.getHeapUsage'); return result.usedSize }
  finally { await cdp.detach() }
}
function report(info: TestInfo, values: Record<string, unknown>) {
  writeFileSync(info.outputPath('performance.json'), JSON.stringify({ timestamp: new Date().toISOString(), project: info.project.name, node: process.version, platform: process.platform, input: 'Existing backend captures; only JSON whitespace and log identity capacity variants', sourceHead: execFileSync('git', ['-c', 'safe.directory=D:/JOTO_Projects/LocalDataMiddlePlatform', 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), ...values }, null, 2))
}

test('F12 capacity records captured pagination and bounded large JSON timings', async ({ page, browser }, info) => {
  await setup(page)
  const first = paginationCapture('students.query', 1), second = paginationCapture('students.query', 2)
  let calls = 0
  await page.route('**/console-api/query/api/v1/sicp/students/query', route => {
    const body = route.request().postDataJSON(), f = body.page === 2 ? second : first; calls++
    expect(body).toEqual(f.request)
    expect(route.request().headers()['x-query-snapshot'] ?? '').toBe(body.page === 2 ? first.headers['X-Query-Snapshot'] : '')
    // JSON whitespace only: unchanged captured records and metadata.
    return route.fulfill({ status: f.status, headers: f.headers, contentType: 'application/json', body: JSON.stringify(f.response) + ' '.repeat(256 * 1024) })
  })
  let start = Date.now()
  await page.goto('/console/apis/students.query')
  await expect(page.getByRole('button', { name: '在线测试', exact: true })).toBeEnabled()
  const firstLoadMs = Date.now() - start, baselineHeap = await memory(page)
  expect(firstLoadMs).toBeLessThan(10000)
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(first.request))
  start = Date.now(); await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  const pre = dialog.getByLabel('响应原文', { exact: true })
  await expect(pre).toBeVisible()
  const queryMs = Date.now() - start; expect(queryMs).toBeLessThan(5000)
  expect((await pre.textContent())!.length).toBe(4096)
  start = Date.now(); await dialog.getByRole('button', { name: '展开内容（最多 65536 字符）', exact: true }).click()
  await expect.poll(async () => (await pre.textContent())!.length).toBe(65536)
  const expandMs = Date.now() - start; expect(expandMs).toBeLessThan(3000)
  start = Date.now(); await dialog.getByRole('region', { name: '本次分页', exact: true }).getByRole('button', { name: '下一页', exact: true }).click()
  await expect(dialog.getByRole('region', { name: '本次分页', exact: true })).toContainText('第 2 /')
  const nextPageMs = Date.now() - start; expect(nextPageMs).toBeLessThan(5000); expect(calls).toBe(2)
  const peakHeap = await memory(page); expect(peakHeap).toBeLessThan(128 * mib)
  await page.keyboard.press('Escape'); await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(page.getByLabel('管理员账号', { exact: true })).toBeVisible()
  const releasedHeap = await memory(page), domElements = await page.locator('*').count()
  expect(releasedHeap - baselineHeap).toBeLessThan(16 * mib); expect(domElements).toBeLessThan(2000)
  report(info, { browser: browser.version(), firstLoadMs, queryMs, expandMs, nextPageMs, baselineHeap, peakHeap, releasedHeap, domElements })
})

test('F12 capacity records 1000 log frames and release on navigation', async ({ page, browser }, info) => {
  await setup(page)
  const initial = recordedLogPage(), item = initial.data.items[0]
  initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.stream_cursor = 'capacity-start'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  let streams = 0
  const body = Array.from({ length: 1000 }, (_, i) => `id: capacity-${i}\nevent: event\ndata: ${JSON.stringify({ ...item, event_id: (i + 1).toString(16).padStart(64, '0'), sequence: String(22 + i) })}\n\n`).join('') + 'event: control\ndata: {"code":"forbidden","retryable":false,"rebuild_required":false}\n\n'
  await page.route('**/admin/v1/logs/platform/stream?*', route => { streams++; return route.fulfill({ contentType: 'text/event-stream', body }) })
  await page.goto('/console/logs'); const baselineHeap = await memory(page)
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  let start = Date.now(); await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  const stream = page.getByRole('region', { name: '实时日志接收' })
  await expect(stream).toContainText('已接收 1000 条新增事件')
  const receiveMs = Date.now() - start; expect(receiveMs).toBeLessThan(5000)
  await expect(stream).toContainText('已展示 0 条 · 待展示 999 条 · 实时剩余预算 999 条')
  start = Date.now(); await stream.getByRole('button', { name: '显示 999 条新日志', exact: true }).click()
  await expect(stream.getByRole('listitem')).toHaveCount(499)
  const renderMs = Date.now() - start; expect(renderMs).toBeLessThan(3000)
  const peakHeap = await memory(page); expect(peakHeap).toBeLessThan(128 * mib)
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  await expect(stream).toHaveCount(0)
  const releasedHeap = await memory(page), domElements = await page.locator('*').count()
  expect(releasedHeap - baselineHeap).toBeLessThan(16 * mib); expect(domElements).toBeLessThan(2000); expect(streams).toBe(1)
  report(info, { browser: browser.version(), receiveMs, renderMs, baselineHeap, peakHeap, releasedHeap, domElements, streams, frames: 1000 })
})