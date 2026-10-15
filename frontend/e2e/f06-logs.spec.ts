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
  await expect(page.getByRole('region', { name: '所选日志详情' })).toContainText(/"redacted":\s*true/)
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
test('F08 phase filter and explicit correlation preserve recorded event boundaries', async ({ page }) => {
  await setup(page)
  const response = recordedLogPage(), id = response.data.items[0].request_id; let calls = 0
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    calls++; const q = new URL(route.request().url()).searchParams
    expect(q.get('phase')).toBe('started'); expect(q.get('id_format')).toBe('correlation'); expect(q.get('request_id')).toBe(id)
    response.data.filter.phase = 'started'; response.data.filter.request_id = id
    return route.fulfill({ json: response })
  })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '事件阶段', exact: true }).selectOption('started')
  await page.getByLabel('关联标识', { exact: true }).fill(id)
  await page.getByRole('combobox', { name: '标识格式', exact: true }).selectOption('correlation')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toBeVisible()
  expect(calls).toBe(1)
  await page.getByLabel('开始时间', { exact: true }).fill('2026-10-01T00:00')
  await page.getByLabel('结束时间', { exact: true }).fill('2026-10-14T00:00')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  await expect(page.getByRole('alert').getByText(/范围最多 7 天/)).toBeVisible(); expect(calls).toBe(1)
})
test('F08 direct log detail shows captured facts and exact sanitized tokens and preserves them on failure', async ({ page }, info) => {
  await setup(page)
  const f = managementCapture('log_detail'), d = f.response.data as any; let wrong = false
  await page.route(`**${f.path}`, route => route.fulfill({ status: f.status, json: wrong ? { ...f.response, data: { ...d, sequence: '22' } } : f.response }))
  await page.goto(`/console/logs/${d.source}/events/${d.event_id}`)
  await expect(page.getByRole('heading', { name: '日志详情', exact: true })).toBeVisible()
  await expect(page.getByText('当前事件未记录结果', { exact: true })).toBeVisible()
  await expect(page.getByLabel('日志脱敏详情', { exact: true })).toHaveText(JSON.stringify(d))
  await expect(page.getByText('当前事件没有结束边界', { exact: false })).toBeVisible()
  await expect(page.getByText('发生时间', { exact: true })).toBeVisible()
  await expect(page.getByText('入库时间', { exact: true })).toBeVisible()
  wrong = true; await page.getByRole('button', { name: '重新读取日志详情', exact: true }).click()
  await expect(page.getByText('保留上次确认的事件内容', { exact: false })).toBeVisible()
  await expect(page.getByLabel('日志脱敏详情', { exact: true })).toHaveText(JSON.stringify(d))
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('log-detail-mobile.png'), fullPage: true })
})

test('F08 missing log detail explains retention without inventing success or querying another source', async ({ page }) => {
  await setup(page)
  const f = managementCapture('log_detail'), d = f.response.data as any; let calls = 0
  await page.route(`**${f.path}`, route => { calls++; return route.fulfill({ status: 404, json: { code: 'not_found', data: null } }) })
  await page.goto(`/console/logs/${d.source}/events/${d.event_id}`)
  await expect(page.getByText('该事件暂不可查询，可能已过保留期或来源已变化；不能据此判断从未执行。', { exact: true })).toBeVisible()
  await expect(page.getByLabel('日志脱敏详情', { exact: true })).toHaveCount(0)
  expect(calls).toBe(1)
  await page.getByRole('link', { name: '← 返回此源日志', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/logs\/platform$/)
})
test('F08 call association missing IDs never fabricates attempts or issues a broad scan', async ({ page }) => {
  await setup(page)
  // Kind/source boundary applied to the captured DTO; no generated upstream business rows are added.
  const fixture = managementCapture('log_detail'), d = fixture.response.data as any
  d.source = 'upstream'; d.kind = 'call'
  let scans = 0
  await page.route('**/admin/v1/logs/upstream/events?*', route => { scans++; return route.abort() })
  await page.route(`**/admin/v1/logs/upstream/events/${d.event_id}`, route => route.fulfill({ json: fixture.response }))
  await page.goto(`/console/logs/upstream/events/${d.event_id}`)
  const related = page.getByRole('region', { name: '逻辑调用逐次尝试' })
  await expect(related).toContainText('无法可靠读取逐次尝试')
  await expect(related.getByRole('button', { name: '读取调用尝试' })).toHaveCount(0)
  expect(scans).toBe(0)
})

test('F08 call association scans explicit bounded batches and keeps empty evidence partial', async ({ page }) => {
  await setup(page)
  // Association metadata boundary: the ordinary source result remains the existing empty upstream page.
  const fixture = managementCapture('log_detail'), d = fixture.response.data as any
  d.source = 'upstream'; d.kind = 'call'; d.call_id = d.request_id
  await page.route(`**/admin/v1/logs/upstream/events/${d.event_id}`, route => route.fulfill({ json: fixture.response }))
  const urls: URL[] = []; let first: any
  await page.route('**/admin/v1/logs/upstream/events?*', route => {
    const url = new URL(route.request().url()); urls.push(url)
    const response = first ? structuredClone(first) : recordedLogPage('upstream')
    if (!first) {
      response.data.filter = { ...response.data.filter, ...Object.fromEntries(url.searchParams), page_size: 50 }
      response.data.next_cursor = 'attempt-scan-next'; first = structuredClone(response)
    } else response.data.next_cursor = null
    return route.fulfill({ json: response })
  })
  await page.goto(`/console/logs/upstream/events/${d.event_id}`)
  const related = page.getByRole('region', { name: '逻辑调用逐次尝试' })
  await expect(related.getByRole('button', { name: '读取调用尝试', exact: true })).toBeVisible()
  expect(urls).toHaveLength(0)
  await related.getByRole('button', { name: '读取调用尝试', exact: true }).click()
  await expect(related).toContainText('还有未读取的事件，当前为部分结果')
  expect(urls[0]!.searchParams.get('request_id')).toBe(d.request_id)
  expect(urls[0]!.searchParams.get('id_format')).toBe('correlation')
  expect(urls[0]!.searchParams.has('call_id')).toBe(false)
  await related.getByRole('button', { name: '继续读取尝试' }).click()
  await expect(related).toContainText('仍不代表完整调用生命周期')
  expect([...urls[1]!.searchParams.keys()]).toEqual(['cursor'])
  await expect(related).toContainText('不能据此判断未执行或调用成功')
})
test('F08 update task opens source logs using raw ID without a task write or URL filter', async ({ page }) => {
  await setup(page)
  const task = managementCapture('update_detail'), id = (task.response.data as any).task_id
  await page.route(`**/admin/v1/update-events/${id}`, route => route.fulfill({ json: task.response }))
  await page.route(`**/admin/v1/update-events/${id}/operations?*`, route => route.fulfill({ json: managementCapture('operation_nodes').response }))
  const urls: URL[] = []
  await page.route('**/admin/v1/logs/upstream/events?*', route => {
    urls.push(new URL(route.request().url()))
    const envelope = recordedLogPage('upstream'); envelope.data.filter.task_id = (managementCapture('log_detail').response.data as any).request_id
    return route.fulfill({ json: envelope })
  })
  await page.goto(`/console/updates/${id}`)
  await page.getByRole('button', { name: '查看上游采集日志', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/logs\/upstream$/)
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  expect(urls[0]!.searchParams.get('task_id')).toBe(id)
  expect(urls[0]!.searchParams.get('id_format')).toBe('raw')
  expect(new URL(page.url()).search).toBe('')
})

test('F08 detail uses correlation for cross-source logs and source failure never becomes no execution', async ({ page }) => {
  await setup(page)
  const fixture = managementCapture('log_detail'), d = fixture.response.data as any
  await page.route(`**${fixture.path}`, route => route.fulfill({ json: fixture.response }))
  const urls: URL[] = []
  await page.route('**/admin/v1/logs/upstream/events?*', route => { urls.push(new URL(route.request().url())); return route.fulfill({ status: 503, json: { code: 'unavailable' } }) })
  await page.goto(`/console/logs/platform/events/${d.event_id}`)
  await page.getByRole('button', { name: '同请求日志 · 上游', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/logs\/upstream$/)
  await expect(page.getByRole('alert')).toBeVisible()
  expect(urls[0]!.searchParams.get('request_id')).toBe(d.request_id)
  expect(urls[0]!.searchParams.get('id_format')).toBe('correlation')
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toHaveCount(0)
  await page.goBack()
  await expect(page.getByText('当前事件未记录结果', { exact: true })).toBeVisible()
})
test('F08 sanitized log text remains inert and bounded and copy equals only visible content', async ({ page }) => {
  await setup(page)
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as any).copiedLogText = text } } }))
  const fixture = managementCapture('log_detail'), d = fixture.response.data as any
  // Adversarial display boundary only: retain DTO identity and use a redacted marker, no real credentials.
  d.response = { body: '<img src=x onerror="window.injected=true"> Authorization: [REDACTED] ' + '边界'.repeat(40000), truncated: true }
  d.redaction.truncated = true
  await page.route(`**${fixture.path}`, route => route.fulfill({ json: fixture.response }))
  await page.goto(`/console/logs/platform/events/${d.event_id}`)
  const text = page.getByLabel('日志脱敏详情', { exact: true })
  await expect(text).toContainText('Authorization: [REDACTED]')
  expect((await text.textContent())!.length).toBe(4096)
  expect(await page.evaluate(() => (window as any).injected)).toBeUndefined()
  await expect(page.locator('main img')).toHaveCount(0)
  await page.getByRole('button', { name: '复制当前显示文本', exact: true }).click()
  expect(await page.evaluate(() => (window as any).copiedLogText)).toBe(await text.textContent())
  await page.getByRole('button', { name: '展开内容（最多 65536 字符）', exact: true }).click()
  expect((await text.textContent())!.length).toBe(65536)
  await page.getByRole('button', { name: '复制当前显示文本', exact: true }).click()
  expect(await page.evaluate(() => (window as any).copiedLogText)).toBe(await text.textContent())
  await page.getByRole('link', { name: '数据源', exact: true }).click()
  await expect(text).toHaveCount(0)
  expect(await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]))).not.toContain('[REDACTED]')
})
test('F09 live bootstrap fixes server lower bound and history uses only next cursor', async ({ page }) => {
  await setup(page)
  const first = recordedLogPage(); first.data.filter.mode = 'live'; first.data.filter.to = null
  first.data.stream_cursor = 'stream-start'; first.data.next_cursor = 'history-next'
  const urls: URL[] = []
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    const url = new URL(route.request().url()); urls.push(url)
    const response = structuredClone(first)
    if (url.searchParams.has('cursor')) { response.data.items = []; response.data.next_cursor = null }
    return route.fulfill({ json: response })
  })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await expect(page.getByLabel('结束时间', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  await expect(page.getByText('实时（无固定结束时间）', { exact: true })).toBeVisible()
  expect(urls[0]!.searchParams.get('mode')).toBe('live')
  expect(urls[0]!.searchParams.has('to')).toBe(false)
  await page.getByRole('button', { name: '下一批日志', exact: true }).click()
  await expect(page.getByText('本批没有匹配的日志事件', { exact: true })).toBeVisible()
  expect([...urls[1]!.searchParams.entries()]).toEqual([['cursor', 'history-next']])
})
test('F09 complete SSE event is deduplicated and only complete checkpoint resumes the stream', async ({ page }) => {
  await setup(page)
  const initial = recordedLogPage(), item = initial.data.items[0]
  initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.high_water = '0'; initial.data.items = []; initial.data.stream_cursor = 'initial-live'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  const cursors: string[] = []
  await page.route('**/admin/v1/logs/platform/stream?*', route => {
    cursors.push(new URL(route.request().url()).searchParams.get('cursor')!)
    expect(route.request().headers()['last-event-id']).toBe(cursors.at(-1))
    const event = `id: event-complete\r\nevent: event\r\ndata: ${JSON.stringify(item)}\r\n\r\n`
    const body = cursors.length === 1 ? event + event + ': heartbeat\n\nid: checkpoint-complete\nevent: checkpoint\ndata: {"sequence":"22"}\n\nid: partial\nevent: checkpoint\ndata: {"sequence":"23"}' : 'event: control\ndata: {"code":"forbidden","retryable":false,"rebuild_required":false}\n\n'
    return route.fulfill({ contentType: 'text/event-stream', body })
  })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  const stream = page.getByRole('region', { name: '实时日志接收' })
  await expect(stream).toContainText('已接收 1 条新增事件')
  await expect(stream.getByRole('link', { name: '查看新增事件详情' })).toHaveCount(0)
  await stream.getByRole('button', { name: '显示 1 条新日志', exact: true }).click()
  await expect(stream.getByRole('link', { name: '查看新增事件详情' })).toHaveCount(1)
  await expect.poll(() => cursors.length).toBe(2)
  expect(cursors).toEqual(['initial-live', 'checkpoint-complete'])
})
test('F09 rate limits honor retry delay and expired position requires explicit gap rebuild', async ({ page }) => {
  await page.clock.install()
  await setup(page)
  const initial = recordedLogPage(); initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.stream_cursor = 'bound-cursor'
  let queries = 0, streams = 0
  await page.route('**/admin/v1/logs/platform/events?*', route => { queries++; return route.fulfill({ json: initial }) })
  await page.route('**/admin/v1/logs/platform/stream?*', route => {
    streams++
    if (streams === 1) return route.fulfill({ status: 429, headers: { 'Retry-After': '3' }, json: { code: 'resource_exhausted' } })
    return route.fulfill({ contentType: 'text/event-stream', body: `event: control\ndata: ${JSON.stringify({ code: streams === 2 ? 'cursor_expired' : 'forbidden', retryable: false, rebuild_required: streams === 2 })}\n\n` })
  })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  const stream = page.getByRole('region', { name: '实时日志接收' })
  await expect(stream).toContainText('3 秒后')
  await page.clock.runFor(2900); expect(streams).toBe(1)
  await page.clock.runFor(200)
  await expect(stream).toContainText('可能存在缺口')
  await page.clock.runFor(60000); expect(streams).toBe(2); expect(queries).toBe(1)
  await stream.getByRole('button', { name: '重新读取实时起点', exact: true }).click()
  await expect.poll(() => queries).toBe(2)
  await expect(page.getByText('已请求新的实时起点；前后范围之间可能存在缺口，请按时间补查历史。', { exact: true })).toBeVisible()
  await expect.poll(() => streams).toBe(3)
  await expect(stream).toContainText('服务端停止本次连接')
  for (const state of ['hidden', 'visible']) await page.evaluate(value => { Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => value }); document.dispatchEvent(new Event('visibilitychange')) }, state)
  await page.clock.runFor(30000); expect(streams).toBe(3)
})
test('F09 changing source aborts old stream and ignores its late event', async ({ page }) => {
  await setup(page)
  const initial = recordedLogPage(), item = initial.data.items[0]
  initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.items = []; initial.data.high_water = '0'; initial.data.stream_cursor = 'old-source'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  let release!: () => void, opened = false, aborted = 0
  page.on('requestfailed', request => { if (request.url().includes('/logs/platform/stream')) aborted++ })
  const gate = new Promise<void>(resolve => { release = resolve })
  await page.route('**/admin/v1/logs/platform/stream?*', async route => { opened = true; await gate; await route.fulfill({ contentType: 'text/event-stream', body: `id: late\nevent: event\ndata: ${JSON.stringify(item)}\n\n` }).catch(() => {}) })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  await expect.poll(() => opened).toBe(true)
  await page.getByRole('combobox', { name: '日志来源', exact: true }).selectOption('upstream')
  release()
  await expect.poll(() => aborted).toBe(1)
  await expect(page.getByRole('region', { name: '实时日志接收' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '查看新增事件详情' })).toHaveCount(0)
  await expect(page.getByText('尚未查询持久日志', { exact: true })).toBeVisible()
})

test('F09 streaming session expiration clears logs and never reconnects', async ({ page }) => {
  await page.clock.install(); await setup(page)
  const initial = recordedLogPage(); initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.stream_cursor = 'expired-session'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  let calls = 0
  await page.route('**/admin/v1/logs/platform/stream?*', route => { calls++; return route.fulfill({ status: 401, json: { code: 'unauthenticated' } }) })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  await expect(page.getByLabel('管理员账号', { exact: true })).toBeVisible()
  await page.clock.runFor(60000)
  expect(calls).toBe(1)
  await expect(page.getByRole('region', { name: '实时日志接收' })).toHaveCount(0)
  await expect(page.getByRole('region', { name: '持久调用日志表格' })).toHaveCount(0)
})
test('F09 summary budget includes history and pending rows without moving the read position', async ({ page }, info) => {
  await page.clock.install(); await setup(page)
  const initial = recordedLogPage(), item = initial.data.items[0]
  initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.stream_cursor = 'capacity-start'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  let streams = 0
  await page.route('**/admin/v1/logs/platform/stream?*', route => {
    streams++
    // Capacity-only protocol variants retain all captured business fields; not normal fixture records.
    const body = streams <= 2 ? Array.from({ length: 600 }, (_, offset) => {
      const n = (streams - 1) * 600 + offset + 1
      return `id: capacity-${n}\nevent: event\ndata: ${JSON.stringify({ ...item, event_id: n.toString(16).padStart(64, '0'), sequence: String(21 + n) })}\n\n`
    }).join('') : 'event: control\ndata: {"code":"forbidden","retryable":false,"rebuild_required":false}\n\n'
    return route.fulfill({ contentType: 'text/event-stream', body })
  })
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  const stream = page.getByRole('region', { name: '实时日志接收' }), list = stream.getByRole('list', { name: '已展示实时事件' })
  await expect(stream).toContainText('已接收 600 条新增事件')
  await expect(list.getByRole('listitem')).toHaveCount(0)
  await stream.getByRole('button', { name: '显示 600 条新日志', exact: true }).click()
  await expect(list.getByRole('listitem')).toHaveCount(499)
  const first = await list.getByRole('link').first().getAttribute('href')
  await list.evaluate(el => { el.scrollTop = 100 })
  await page.clock.runFor(1100)
  await expect(stream).toContainText('已接收 1200 条新增事件')
  await expect(stream).toContainText('已展示 499 条 · 待展示 500 条 · 实时剩余预算 999 条')
  expect(await list.getByRole('link').first().getAttribute('href')).toBe(first)
  expect(await list.evaluate(el => el.scrollTop)).toBe(100)
  await expect(stream).toContainText('201 条摘要被裁剪')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('live-budget-mobile.png'), fullPage: true })
  await stream.getByRole('button', { name: '转到历史补查', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '查询模式', exact: true })).toHaveValue('history')
  await expect(stream).toHaveCount(0)
})
test('F09 hidden page suspends stream retry and resumes the last checkpoint without bypassing rate limit', async ({ page }) => {
  await page.clock.install(); await setup(page)
  const initial = recordedLogPage(); initial.data.filter.mode = 'live'; initial.data.filter.to = null; initial.data.stream_cursor = 'visibility-start'
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ json: initial }))
  const cursors: string[] = []
  await page.route('**/admin/v1/logs/platform/stream?*', route => {
    cursors.push(new URL(route.request().url()).searchParams.get('cursor')!)
    if (cursors.length === 2) return route.fulfill({ status: 429, headers: { 'Retry-After': '10' }, json: { code: 'resource_exhausted' } })
    return route.fulfill({ contentType: 'text/event-stream', body: 'id: visibility-checkpoint\nevent: checkpoint\ndata: {"sequence":"21"}\n\n' })
  })
  const visibility = async (state: string) => page.evaluate(value => { Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => value }); document.dispatchEvent(new Event('visibilitychange')) }, state)
  await page.goto('/console/logs')
  await page.getByRole('combobox', { name: '查询模式', exact: true }).selectOption('live')
  await page.getByRole('button', { name: '查询日志首批', exact: true }).click()
  const stream = page.getByRole('region', { name: '实时日志接收' })
  await expect(stream).toContainText('1 秒后')
  await visibility('hidden'); await page.clock.runFor(60000); expect(cursors).toHaveLength(1)
  await visibility('visible'); await expect(stream).toContainText('10 秒后')
  expect(cursors).toEqual(['visibility-start', 'visibility-checkpoint'])
  await visibility('hidden'); await page.clock.runFor(2000); await visibility('visible')
  await page.clock.runFor(7000); expect(cursors).toHaveLength(2)
  await page.clock.runFor(1500); await expect.poll(() => cursors.length).toBe(3)
  expect(cursors[2]).toBe('visibility-checkpoint')
})