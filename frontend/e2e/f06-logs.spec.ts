import { test, expect, type Page } from '@playwright/test'
import { backendCapture, backendCatalog, managementCapture } from '../fixtures/backend'
import { recordedLogPage } from '../fixtures/logTests'
import { sourceJobResult } from '../fixtures/sourceTests'
async function setup(page: Page) {
  const session = managementCapture('session')
  await page.route('**/console-api/session', route => route.fulfill({ status: session.status, json: session.response }))
  await page.route('**/console-api/**/livez', route => route.fulfill({ json: { live: true } }))
  await page.route('**/console-api/**/readyz', route => route.fulfill({ json: { ready: true, data_ready: true } }))
  for (const name of ['overview', 'system', 'sources']) {
    const f = managementCapture(name)
    await page.route(`**${f.path}`, route => route.fulfill({ status: f.status, json: f.response }))
  }
  const items = backendCatalog() as { id: string; version: string }[]
  await page.route('**/admin/v1/apis', route => route.fulfill({ json: { code: 'ok', data: { version: items[0]!.version, items } } }))
  await page.route('**/admin/v1/apis/*', route => route.fulfill({ json: { code: 'ok', data: items.find(i => i.id === decodeURIComponent(new URL(route.request().url()).pathname.split('/').at(-1)!)) } }))
  await page.route('**/admin/v1/logs/**', route => route.abort())
}
async function business(page: Page) {
  const f = backendCapture('students.query')
  await page.route('**/console-api/query/api/v1/sicp/students/query', route => route.fulfill({ status: f.status, headers: f.headers, json: f.response }))
  await page.goto('/console/apis/students.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(f.request))
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect(dialog.getByRole('status', { name: '测试状态', exact: true })).toContainText('查询成功')
  return f
}
test('F06 log handoff from business response supports stable batches and existing sanitized detail', async ({ page }, info) => {
  await setup(page)
  const urls: URL[] = [], first = recordedLogPage()
  first.data.filter.request_id = first.data.items[0].request_id
  first.data.next_cursor = 'protocol-next'
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    const url = new URL(route.request().url()); urls.push(url)
    const response = url.searchParams.has('cursor') ? structuredClone(first) : first
    if (url.searchParams.has('cursor')) { response.data.items = []; response.data.next_cursor = null }
    return route.fulfill({ json: response })
  })
  const d = managementCapture('log_detail')
  await page.route(`**${d.path}`, route => route.fulfill({ status: d.status, json: d.response }))
  const fixture = await business(page)
  await page.getByRole('dialog').getByRole('button', { name: '查看本次调用日志' }).click()
  await expect(page).toHaveURL(/\/console\/logs\/platform$/)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toBeVisible()
  expect(urls[0]!.searchParams.get('request_id')).toBe(fixture.headers['X-Request-ID'])
  expect(urls[0]!.searchParams.get('test_run_id')).toBeNull()
  expect(urls[0]!.searchParams.get('id_format')).toBe('raw')
  await page.getByRole('button', { name: '查看日志详情' }).click()
  await expect(page.getByRole('region', { name: '所选日志详情' })).toContainText('"redacted": true')
  await page.getByRole('button', { name: '下一批日志' }).click()
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  expect([...urls[1]!.searchParams.keys()]).toEqual(['cursor'])
  await expect(page.getByRole('button', { name: '下一批日志' })).toBeDisabled()
  await page.getByRole('button', { name: '上一批日志' }).click()
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toBeVisible()
  expect(urls).toHaveLength(2)
  expect(await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]))).not.toContain(fixture.headers['X-Request-ID'])
  await page.screenshot({ path: info.outputPath('logs-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('logs-mobile.png'), fullPage: true })
  await page.reload()
  await expect(page.getByLabel('关联标识', { exact: true })).toHaveValue('')
  await expect(page.getByText('尚未查询持久日志', { exact: true })).toBeVisible()
  expect(urls).toHaveLength(2)
})
test('F06 source log handoff uses accepted job ID without repeating a source test', async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), result = sourceJobResult('dwd_bzkscjzlb')
  let posts = 0
  await page.route('**/admin/v1/api-tests', route => { posts++; return route.fulfill({ status: accepted.status, headers: { Location: result.path }, json: accepted.response }) })
  await page.route(`**${result.path}`, route => route.fulfill({ status: result.status, body: result.raw, contentType: 'application/json' }))
  const urls: URL[] = []
  await page.route('**/admin/v1/logs/upstream/events?*', route => {
    urls.push(new URL(route.request().url()))
    const p = recordedLogPage('upstream')
    // Protocol identity echo uses the existing backend correlation example, no rows invented.
    p.data.filter.test_run_id = (managementCapture('log_detail').response.data as any).event_id
    return route.fulfill({ json: p })
  })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(dialog.getByRole('status', { name: '测试状态', exact: true })).toContainText('源测试成功')
  await dialog.getByRole('button', { name: '查看本次调用日志' }).click()
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  expect(urls[0]!.searchParams.get('test_run_id')).toBe((accepted.response.data as { id: string }).id)
  expect(urls[0]!.searchParams.has('request_id')).toBe(false)
  expect(urls[0]!.searchParams.has('api_id')).toBe(false)
  expect(posts).toBe(1)
})
test('F06 recent summary opens logs and server filters reset the old batch', async ({ page }) => {
  await setup(page)
  const paths: URL[] = []
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    const u = new URL(route.request().url()); paths.push(u)
    const p = recordedLogPage(); p.data.items = []
    if (u.searchParams.has('request_id')) p.data.filter.request_id = (managementCapture('log_detail').response.data as any).request_id
    for (const k of ['outcome', 'operation', 'from', 'to']) if (u.searchParams.has(k)) p.data.filter[k] = u.searchParams.get(k)
    return route.fulfill({ json: p })
  })
  await business(page)
  await page.getByRole('button', { name: '关闭测试', exact: true }).click()
  await page.getByRole('tab', { name: '调用记录', exact: true }).click()
  await page.getByRole('region', { name: '近期测试摘要' }).getByRole('button', { name: '查看本次调用日志' }).click()
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  const before = paths.length
  await page.getByRole('combobox', { name: '调用结果', exact: true }).selectOption('failed')
  await expect(page.getByRole('region', { name: '持久调用日志', exact: true })).toHaveCount(0)
  expect(paths).toHaveLength(before)
  await page.getByLabel('开始时间', { exact: true }).fill('2026-10-12T08:00')
  await page.getByLabel('结束时间', { exact: true }).fill('2026-10-13T08:00')
  await page.getByRole('button', { name: '查询日志首批' }).click()
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  expect(paths.at(-1)!.searchParams.get('outcome')).toBe('failed')
  expect(paths.at(-1)!.searchParams.has('cursor')).toBe(false)
  expect(paths.at(-1)!.searchParams.has('from')).toBe(true)
})
test('F06 log pagination rejects drift and expired cursors without replacing confirmed rows', async ({ page }) => {
  await setup(page)
  const p = recordedLogPage(); p.data.next_cursor = 'protocol-next'
  let calls = 0
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    calls++
    if (calls === 4) return route.fulfill({ status: 409, json: { code: 'cursor_expired', message: 'cursor expired', data: null } })
    const response = structuredClone(p)
    if (calls === 2) { response.data.items = []; response.data.high_water = '22' }
    if (calls === 3) response.data.items = []
    return route.fulfill({ json: response })
  })
  await page.goto('/console/logs')
  await page.getByRole('button', { name: '查询日志首批' }).click()
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toBeVisible()
  await page.getByRole('button', { name: '下一批日志' }).click()
  await expect(page.getByText(/日志响应范围、标识或分页顺序/)).toBeVisible()
  await page.getByRole('button', { name: '下一批日志' }).click()
  await expect.poll(() => calls).toBe(3)
  await expect(page.getByText(/日志响应范围、标识或分页顺序/)).toBeVisible()
  await page.getByRole('button', { name: '下一批日志' }).click()
  await expect(page.getByText(/查询游标已过期/)).toBeVisible()
  await expect(page.getByRole('button', { name: '下一批日志' })).toBeDisabled()
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toBeVisible()
  expect(calls).toBe(4)
})
test('F06 changing log filters cancels late reads and leaving clears identifiers', async ({ page }) => {
  await setup(page)
  let release!: () => void, started!: () => void
  const gate = new Promise<void>(resolve => { release = resolve }), entered = new Promise<void>(resolve => { started = resolve })
  await page.route('**/admin/v1/logs/platform/events?*', async route => { started(); await gate; await route.fulfill({ json: recordedLogPage() }).catch(() => {}) })
  await page.goto('/console/logs')
  await page.getByRole('button', { name: '查询日志首批' }).click(); await entered
  await page.getByLabel('关联标识', { exact: true }).fill((managementCapture('log_detail').response.data as any).request_id)
  release()
  await expect(page.getByText('尚未查询持久日志', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toHaveCount(0)
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  await expect(page.getByRole('heading', { name: '接口中心', exact: true })).toBeVisible()
  await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '日志中心', exact: true }).click()
  await expect(page.getByLabel('关联标识', { exact: true })).toHaveValue('')
})

test('F08 four log views send backend source and kind and clear old batches without auto-query', async ({ page }, info) => {
  await setup(page)
  const queries: URL[] = []
  await page.route('**/admin/v1/logs/*/events?*', route => {
    const url = new URL(route.request().url()); queries.push(url)
    const response = recordedLogPage(url.pathname.includes('/upstream/') ? 'upstream' : 'platform')
    response.data.filter.kind = url.searchParams.get('kind') || undefined
    if (response.data.filter.kind !== 'request') response.data.items = []
    return route.fulfill({ json: response })
  })
  await page.goto('/console/logs')
  const views = page.getByRole('navigation', { name: '日志视图', exact: true })
  let count = 0
  for (const [name, source, kind] of [['接口调用', 'platform', 'request'], ['更新操作', 'platform', 'task'], ['原数据接口', 'upstream', ''], ['管理审计', 'platform', 'audit']]) {
    await views.getByRole('button', { name, exact: true }).click()
    await expect(page.getByText('尚未查询持久日志', { exact: true })).toBeVisible(); expect(queries).toHaveLength(count)
    await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
    await expect(page.getByRole('region', { name: '持久调用日志', exact: true })).toBeVisible()
    count++; expect(queries).toHaveLength(count); expect(queries.at(-1)!.pathname).toContain(`/logs/${source}/events`)
    expect(queries.at(-1)!.searchParams.get('kind')).toBe(kind || null)
  }
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('log-views-mobile.png'), fullPage: true })
})