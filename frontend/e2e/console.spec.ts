import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { backendCapture, backendCatalog, managementCapture } from '../fixtures/backend'
import { sourceJobResult } from '../fixtures/sourceTests'
import { operations, queryBody, cell } from '../src/queryOperations'

test.beforeEach(async ({ page }) => {
  await page.route('**/admin/v1/logs/**', route => route.abort())
  const items = backendCatalog() as { id: string; version: string }[]
  await page.route('**/admin/v1/apis', route => route.fulfill({ json: { code: 'ok', message: '', data: { version: items[0]!.version, items } } }))
  await page.route('**/admin/v1/apis/*', route => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').at(-1)!)
    const api = items.find(item => item.id === id)
    return route.fulfill({ status: api ? 200 : 404, json: { code: api ? 'ok' : 'not_found', data: api ?? null } })
  })
})
async function showQueries(page: Page) {
  const opener = page.getByRole('button', { name: '业务数据核验', exact: true })
  const heading = page.getByRole('heading', { name: '业务查询', exact: true })
  await expect(opener.or(heading)).toBeVisible()
  if (await opener.isVisible()) await opener.click()
  await expect(heading).toBeVisible()
}
async function openQueries(page: Page) {
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  await showQueries(page)
}

test('BT09 real work, unknown totals, stalls and final timers', async ({ page }) => {
  const start = new Date('2026-10-08T08:00:00Z')
  await page.clock.install({ time: start })
  await setup(page)
  let phase = 'writing_table', status = 'running', instance = 'M', known = true
  let completed = 500, unit = 'rows', finished: string | undefined
  const task = () => ({ task_id: 'actual-progress', mode: 'initialize', status, accepted_at: start.toISOString(), started_at: start.toISOString(), finished_at: finished })
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [task()] } } }))
  await page.route('**/admin/v1/tasks/actual-progress', async route => {
    const heartbeat = await page.evaluate(() => new Date().toISOString())
    await route.fulfill({ json: { code: 'ok', data: { ...task(), work_progress: { phase, instance, unit, completed, total: 1000, total_known: known, attempt: 2, cumulative_completed: 1500, cumulative_elapsed_ms: 60000, started_at: start.toISOString(), heartbeat_at: heartbeat, last_progress_at: start.toISOString() } } } })
  })
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await page.getByRole('button', { name: /actual-progress/ }).click()
  const stage = page.getByRole('region', { name: '当前执行阶段' })
  await expect(stage.getByRole('progressbar')).toHaveAttribute('value', '500')
  await expect(stage).toContainText('第 2 次执行')
  await page.clock.runFor(51000)
  await expect(stage).toContainText('执行器在线，但超过 45 秒没有已确认的工作量进展')
  completed = 1000
  await page.clock.runFor(5100)
  await expect(stage).toContainText('100%')
  await expect(stage).toContainText('仍需等待完整校验与发布')
  for (const next of ['content_check', 'business_check', 'draining', 'publishing']) {
    phase = next; instance = 'S'; known = false; unit = next === 'business_check' ? 'cases' : ''
    await page.clock.runFor(5100)
    await expect(stage.getByRole('progressbar')).toHaveCount(0)
    await expect(stage).toContainText('数据库副本：S')
  }
  phase = 'business_check'; unit = 'cases'; status = 'failed'; finished = await page.evaluate(() => new Date().toISOString())
  await page.clock.runFor(5100)
  await expect(stage).toContainText('失败前阶段：核对业务案例')
  await expect(stage.getByRole('status')).toHaveCount(0)
  const timer = stage.getByText(/任务总耗时/)
  const frozen = await timer.textContent()
  await page.clock.runFor(61000)
  await expect(timer).toHaveText(frozen!)
  // Missing finished_at is an unknown duration, not the last heartbeat.
  finished = undefined
  await page.getByRole('button', { name: '刷新详情', exact: true }).click()
  await expect(timer).toContainText('未知')
  await expect(stage).toContainText('累计量包含重复执行')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})

test('post-acquisition stages show heartbeat, stale execution and publication', async ({ page }, testInfo) => {
  const started = new Date('2026-09-30T08:00:00Z')
  await page.clock.install({ time: started })
  await setup(page)
  let status = 'running', phase = 'building_table', heartbeat = started.toISOString()
  let table: string | undefined = 'dwd_bzkscjzlb', instance: string | undefined
  let tables: string[] | undefined
  const task = () => ({ task_id: 'build-progress', mode: 'initialize', status, accepted_at: started.toISOString(), started_at: started.toISOString() })
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [task()] } } }))
  await page.route('**/admin/v1/tasks/build-progress', route => route.fulfill({ json: { code: 'ok', data: { ...task(), work_progress: { phase, table, tables, instance, started_at: started.toISOString(), heartbeat_at: heartbeat }, acquisition: { updated_at: started.toISOString(), tables: [{ table: 'dwd_bzkscjzlb', status: 'collected', completed_pages: 1440, total_pages: 1440 }] } } } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  const stage = page.getByRole('region', { name: '当前执行阶段' })
  await expect(stage.getByRole('heading', { name: '当前阶段：构建表数据' })).toBeVisible()
  await expect(stage).toContainText('dwd_bzkscjzlb')
  await expect(stage).toContainText('执行器在线')
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(stage.getByRole('heading', { name: '当前阶段：构建表数据' })).toBeVisible()
  table = undefined; tables = ['dwd_bzkscjzlb', 'dwd_bzkskbsjzlb']
  await page.clock.runFor(5100)
  await expect(stage).toContainText('当前构建的表：')
  await expect(stage).toContainText('dwd_bzkskbsjzlb')
  await page.clock.runFor(46000)
  await expect(stage).toContainText('状态待确认')
  await expect(stage).toContainText('超过 45 秒未收到执行器心跳')
  heartbeat = await page.evaluate(() => new Date().toISOString())
  phase = 'writing_table'; instance = 'M'; table = 'dwd_bzkscjzlb'; tables = undefined
  await page.clock.runFor(5100)
  await expect(stage.getByRole('heading', { name: '当前阶段：写入数据库' })).toBeVisible()
  await expect(stage).toContainText('数据库副本：M')
  await expect(stage).toContainText('执行器在线')
  await page.screenshot({ path: testInfo.outputPath('build-progress-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('build-progress-mobile.png'), fullPage: true })
  phase = 'publishing'; table = undefined; instance = undefined
  await page.clock.runFor(5100)
  await expect(stage.getByRole('heading', { name: '当前阶段：发布数据版本' })).toBeVisible()
  status = 'published_pending_progress'; phase = 'confirming_progress'
  await page.clock.runFor(5100)
  await expect(stage.getByRole('heading', { name: '当前阶段：确认同步进度' })).toBeVisible()
  status = 'success_published'
  await page.clock.runFor(5100)
  await expect(stage).toContainText('已结束')
  await expect(stage).not.toContainText('执行器在线')
})

test('historical data verification and reuse stay visible as active work', async ({ page }, testInfo) => {
  await page.clock.install()
  await setup(page)
  const task = { task_id: 'reuse-progress', mode: 'initialize', status: 'running', accepted_at: '2026-09-30T07:41:35Z' }
  let phase = 'checking', completed = 0, total: number | null = null, failed = false
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [{ ...task, status: failed ? 'failed' : 'running' }] } } }))
  await page.route('**/admin/v1/tasks/reuse-progress', route => route.fulfill({ json: { code: 'ok', data: { ...task, status: failed ? 'failed' : 'running', acquisition: { updated_at: '2026-09-30T07:42:00Z', tables: [
    { table: 'dwd_bzksbjsjzlb', status: 'collected', completed_pages: 1, total_pages: 1, reused: true },
    { table: 'dwd_bzkscjzlb', status: phase, completed_pages: completed, total_pages: total },
    { table: 'dwd_bzksgrtxsjzlb', status: 'waiting', completed_pages: 0, total_pages: null },
  ] } } } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByText('正在校验历史数据', { exact: true })).toBeVisible()
  await expect(page.getByText('正在检查已保存的分页数据，确认完整后复用。')).toBeVisible()
  await expect(page.getByRole('progressbar')).toHaveCount(0)
  completed = 720; total = 1440
  await page.clock.runFor(10100)
  await expect(page.getByRole('progressbar', { name: 'dwd_bzkscjzlb 历史数据校验进度' })).toHaveAttribute('value', '720')
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByText('等待处理', { exact: true })).toBeVisible()
  phase = 'reusing'; completed = 360
  await page.clock.runFor(5100)
  await expect(page.getByText('正在复用历史数据', { exact: true })).toBeVisible()
  await expect(page.getByText('360 / 1440 页 · 25%', { exact: true })).toBeVisible()
  await expect(page.getByText('已完成 1 / 3 张表', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('reuse-progress-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('reuse-progress-mobile.png'), fullPage: true })
  failed = true
  await page.clock.runFor(5100)
  await expect(page.getByText('历史数据复用已停止', { exact: true })).toBeVisible()
  await expect(page.getByRole('progressbar', { name: 'dwd_bzkscjzlb 历史数据复用进度' })).toHaveAttribute('value', '360')
})

test('failed full update offers retry of incomplete tables and single-table repair', async ({ page }) => {
  await setup(page)
  const task = { task_id: 'failed-full', mode: 'force_full', status: 'failed', failure: 'acquisition_failed', accepted_at: '2026-09-30T10:00:00Z' }
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: true, items: [task] } } }))
  await page.route('**/admin/v1/tasks/failed-full', route => route.fulfill({ json: { code: 'ok', data: { ...task, acquisition: { tables: [
    { table: 'dwd_bzksbjsjzlb', status: 'collected', completed_pages: 1, total_pages: 1 },
    { table: 'dwd_bzkscjzlb', status: 'failed', completed_pages: 9, total_pages: 20 },
  ] } } } }))
  let retries = 0
  await page.route('**/admin/v1/tasks/failed-full/rerun', route => {
    retries++
    expect(route.request().postDataJSON()).toEqual({})
    expect(route.request().headers()['idempotency-key']).toBeTruthy()
    return route.fulfill({ status: 202, json: { code: 'accepted', data: { task_id: 'full-repair' } } })
  })
  await page.route('**/admin/v1/tasks/full-repair', route => route.fulfill({ json: { code: 'ok', data: { task_id: 'full-repair', mode: 'force_full', status: 'accepted' } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await page.getByRole('button', { name: /failed-full/ }).click()
  await expect(page.getByText('部分完成，待补齐', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '强制更新 dwd_bzkscjzlb', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '重试未完成的表', exact: true }).click()
  await expect(page.getByRole('heading', { name: '确认失败重跑', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
  await expect(page.getByText('任务已受理：full-repair。受理不代表执行完成。')).toBeVisible()
  expect(retries).toBe(1)
})

for (const status of ['success_published', 'success_unchanged']) {
  test(`manual incremental sync preserves retries and displays ${status}`, async ({ page }, testInfo) => {
    await setup(page)
    const keys: string[] = []
    let fullCalls = 0
    await page.route('**/admin/v1/tasks/force-full', route => { fullCalls++; return route.abort() })
    await page.route('**/admin/v1/tasks/incremental', route => {
      expect(route.request().postDataJSON()).toEqual({})
      expect(route.request().headers()['x-sicp-csrf']).toBe('synthetic-csrf')
      keys.push(route.request().headers()['idempotency-key']!)
      if (keys.length === 1) return route.abort()
      return route.fulfill({ status: 202, json: { code: 'accepted', data: { task_id: 'manual-sync' } } })
    })
    await page.route('**/admin/v1/tasks/manual-sync', route => route.fulfill({ json: { code: 'ok', data: {
      task_id: 'manual-sync', mode: 'incremental', status, operator: 'admin', progress_confirmed: true,
      accepted_at: '2026-09-30T10:00:00Z', checked_at: '2026-09-30T10:05:00Z',
      published_at: status === 'success_published' ? '2026-09-30T10:05:00Z' : null,
    } } }))
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await page.getByRole('button', { name: '立即增量同步', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('校验通过后自动发布')
    await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('原请求已保留')
    await expect(page.getByRole('radio', { name: /强制全量/ })).toBeDisabled()
    await page.getByRole('button', { name: '重试原请求', exact: true }).click()
    await expect(page.getByRole('heading', { name: '确认增量同步', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
    await expect(page.getByText(status === 'success_published' ? '发布成功' : '检查完成，无变化', { exact: true })).toBeVisible()
    await expect(page.getByText('已确认', { exact: true })).toBeVisible()
    expect(keys).toHaveLength(2)
    expect(keys[1]).toBe(keys[0])
    expect(fullCalls).toBe(0)
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
    await page.screenshot({ path: testInfo.outputPath(`manual-incremental-${status}.png`), fullPage: true })
  })
}

test('manual incremental sync requires initialization and current idle status', async ({ page }) => {
  await setup(page)
  let initialized = false, running = false, unavailable = false
  await page.route('**/admin/v1/tasks/', route => unavailable
    ? route.fulfill({ status: 503, json: { code: 'unavailable' } })
    : route.fulfill({ json: { code: 'ok', data: { initialized, items: running ? [{ task_id: 'other', mode: 'incremental', status: 'running' }] : [] } } }))
  await page.route('**/admin/v1/tasks/other', route => route.fulfill({ json: { code: 'ok', data: { task_id: 'other', mode: 'incremental', status: 'running' } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  const button = page.getByRole('button', { name: '立即增量同步', exact: true })
  await expect(button).toBeDisabled()
  await expect(page.getByText('初始化完成后可手动增量同步；当前可先补齐失败表。')).toBeVisible()
  initialized = true
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(button).toBeEnabled()
  running = true
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(button).toBeDisabled()
  running = false; unavailable = true
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(button).toBeDisabled()
})

test('partial initialization can force only one table while retaining successful tables', async ({ page }, testInfo) => {
  await setup(page)
  const task = { task_id: 'partial-initialization', mode: 'initialize', status: 'failed', failure: 'acquisition_failed', accepted_at: '2026-09-30T05:21:41Z' }
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [task] } } }))
  await page.route('**/admin/v1/tasks/partial-initialization', route => route.fulfill({ json: { code: 'ok', data: { ...task, acquisition: { updated_at: '2026-09-30T06:00:00Z', tables: [
    { table: 'dwd_bzksbjsjzlb', status: 'collected', completed_pages: 1, total_pages: 1, reused: true },
    { table: 'dwd_bzkscjzlb', status: 'failed', completed_pages: 1113, total_pages: 1440 },
  ] } } } }))
  let posted = ''
  await page.route('**/admin/v1/tables/dwd_bzkscjzlb/force-full', route => {
    posted = route.request().url()
    expect(route.request().postDataJSON()).toEqual({})
    expect(route.request().headers()['idempotency-key']).toBeTruthy()
    return route.fulfill({ status: 202, headers: { Location: '/admin/v1/tasks/repair-one-table' }, json: { code: 'accepted', data: { task_id: 'repair-one-table' } } })
  })
  await page.route('**/admin/v1/tasks/repair-one-table', route => route.fulfill({ json: { code: 'ok', data: { ...task, task_id: 'repair-one-table', status: 'accepted', selected_table: 'dwd_bzkscjzlb' } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await page.getByRole('button', { name: /partial-initialization/ }).click()
  await expect(page.getByText('部分完成，待补齐', { exact: true })).toBeVisible()
  await expect(page.getByText('复用已完成数据', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '强制更新 dwd_bzkscjzlb', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('其它表复用最近完成的数据')
  await page.screenshot({ path: testInfo.outputPath('single-table-repair.png'), fullPage: true })
  await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
  await expect(page.getByText('本次强制更新表：dwd_bzkscjzlb', { exact: true })).toBeVisible()
  expect(posted).toContain('/admin/v1/tables/dwd_bzkscjzlb/force-full')
})

test('failed task shows upstream evidence as text and can refresh delayed logs', async ({ page }, testInfo) => {
  await setup(page)
  const task = { task_id: 'diagnostic-task', mode: 'initialize', status: 'failed', failure: 'acquisition_failed', accepted_at: '2026-09-30T05:21:41Z' }
  let unavailable = false
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [task] } } }))
  await page.route('**/admin/v1/tasks/diagnostic-task', route => route.fulfill({ json: { code: 'ok', data: { ...task, diagnostics_unavailable: unavailable, failed_attempts: unavailable ? [] : [{ at: '2026-09-30T05:45:25Z', source: 'dwd_bzkscjzlb', page: 1115, attempt: 3, failure: 'http', http_status: 500, duration_ns: 248000000, response: { body: 'database unavailable <img src=x onerror="window.injected=true">', request_id: 'upstream-trace', truncated: true, read_error: true } }] } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await page.getByRole('button', { name: /diagnostic-task/ }).click()
  const evidence = page.getByRole('region', { name: '采集失败记录' })
  await expect(evidence).toContainText('第 1115 页 · 第 3 次尝试 · HTTP 500')
  await expect(evidence).toContainText('database unavailable')
  await expect(evidence).toContainText('upstream-trace')
  await expect(evidence.locator('img')).toHaveCount(0)
  await expect(evidence).toContainText('响应正文未完整读取')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('failure-diagnostic-mobile.png'), fullPage: true })
  unavailable = true
  await page.getByRole('button', { name: '刷新详情', exact: true }).click()
  await expect(evidence).toContainText('暂时无法读取调用日志')
  await expect(page.getByText('失败原因：acquisition_failed', { exact: true })).toBeVisible()
})

test('table page progress auto-opens, refreshes and preserves the last counts on failure', async ({ page }, testInfo) => {
  await page.clock.install()
  await setup(page)
  const task = { task_id: 'page-progress-task', mode: 'initialize', status: 'running', accepted_at: '2026-09-30T03:08:08Z' }
  let completed = 720, failed = false, unavailable = false, reads = 0
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [{ ...task, status: failed ? 'failed' : 'running' }] } } }))
  await page.route('**/admin/v1/tasks/page-progress-task', route => {
    reads++
    if (unavailable) return route.fulfill({ status: 503, json: { code: 'unavailable' } })
    return route.fulfill({ json: { code: 'ok', data: { ...task, status: failed ? 'failed' : 'running', acquisition: { updated_at: '2026-09-30T04:00:00Z', tables: [
      { table: 'dwd_bzksbjsjzlb', status: 'collected', completed_pages: 1, total_pages: 1 },
      { table: 'dwd_bzkscjzlb', status: 'acquiring', completed_pages: completed, total_pages: 1440 },
      { table: 'dwd_bzksgrtxsjzlb', status: 'waiting', completed_pages: 0, total_pages: null },
    ] } } } })
  })
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByText('720 / 1440 页 · 50%', { exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox')).toBeChecked()
  const bar = page.getByRole('progressbar', { name: 'dwd_bzkscjzlb 采集进度', exact: true })
  await expect(bar).toHaveAttribute('value', '720')
  await page.screenshot({ path: testInfo.outputPath('page-progress-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('page-progress-mobile.png'), fullPage: true })
  completed = 1080
  await page.clock.runFor(5100)
  await expect(page.getByText('1080 / 1440 页 · 75%', { exact: true })).toBeVisible()
  unavailable = true
  await page.clock.runFor(5100)
  await expect(page.getByRole('alert')).toContainText('自动刷新已停止')
  await expect(bar).toHaveAttribute('value', '1080')
  unavailable = false; failed = true
  await page.getByRole('checkbox').click()
  await expect(page.getByText('采集已停止', { exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox')).toBeDisabled()
  const afterFailure = reads
  await page.clock.runFor(10100)
  expect(reads).toBe(afterFailure)
})

test('overview distinguishes unknown totals and completed acquisition from publication', async ({ page }) => {
  await page.clock.install()
  await setup(page)
  const task = { task_id: 'progress-overview', mode: 'force_full', status: 'running', accepted_at: '2026-09-30T03:08:08Z' }
  let collected = false
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: true, items: [task] } } }))
  await page.route('**/admin/v1/tasks/progress-overview', route => route.fulfill({ json: { code: 'ok', data: { ...task, acquisition: { updated_at: '2026-09-30T04:00:00Z', tables: [{ table: 'dwd_bzkscjzlb', status: collected ? 'collected' : 'acquiring', completed_pages: collected ? 1 : 0, total_pages: collected ? 1 : null }] } } } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByText('等待首个分页结果，暂未取得总页数。')).toBeVisible()
  await expect(page.getByRole('progressbar')).toHaveCount(0)
  collected = true
  await page.clock.runFor(10100)
  await expect(page.getByText('所有表采集完成，构建与发布结果请查看任务状态。')).toBeVisible()
  await expect(page.getByRole('heading', { name: '正在全量更新', exact: true })).toBeVisible()
})

test('task page renders with the browser capabilities of its deployment origin', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await setup(page)
  expect(await page.evaluate(() => isSecureContext)).toBe(testInfo.project.name === 'loopback')
  expect(await page.evaluate(() => typeof crypto.randomUUID)).toBe(testInfo.project.name === 'loopback' ? 'function' : 'undefined')
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '强制全量更新', exact: true })).toBeEnabled()
  await expect(page.getByLabel('幂等键', { exact: true })).toHaveValue(/^[a-f0-9]{32}$/)
  expect(errors).toEqual([])
})

test('login errors are clear and mobile login fits the viewport', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAuth(page)
  await page.goto('./#tasks')
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await page.getByLabel('管理员账号').fill('admin')
  await page.getByLabel('密码', { exact: true }).fill('wrong-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('账号或密码不正确')
  await expect(page.getByLabel('密码', { exact: true })).toHaveValue('')
  await expect(page.getByRole('navigation')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('login-mobile.png'), fullPage: true })
})

test('expired session returns to login and removes protected pages', async ({ page }) => {
  await setup(page)
  await page.route('**/console-api/query/api/**', route => route.fulfill({ status: 401, json: { code: 'unauthenticated', data: null } }))
  await openQueries(page)
  await page.getByLabel('学号 *', { exact: true }).fill('001')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('登录已过期')
  await expect(page.getByRole('navigation')).toHaveCount(0)
})

async function setup(page: Page) {
  for (const name of ['overview', 'system', 'sources']) {
    const fixture = managementCapture(name)
    await page.route(`**${fixture.path}`, route => route.fulfill({ status: fixture.status, json: fixture.response }))
  }
  await mockAuth(page)
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { items: [], initialized: true, interval_seconds: 7200 } } }))
  await page.route('**/console-api/**/livez', route => route.fulfill({ json: { live: true } }))
  await page.route('**/console-api/**/readyz', route => route.fulfill({ status: 503, json: { ready: false, data_ready: false } }))
  await page.goto('./')
  await page.getByLabel('管理员账号').fill('admin')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
}

async function mockAuth(page: Page) {
  let authenticated = false
  const data = { operator: 'admin', csrf: 'synthetic-csrf' }
  await page.route('**/console-api/session', route => route.fulfill({ status: authenticated ? 200 : 401, json: { code: authenticated ? 'ok' : 'unauthenticated', data: authenticated ? data : null } }))
  await page.route('**/console-api/login', route => {
    const input = route.request().postDataJSON()
    authenticated = input.username === 'admin' && input.password === 'synthetic-password'
    expect(route.request().headers()['x-sicp-request']).toBe('console')
    return route.fulfill({ status: authenticated ? 200 : 401, json: { code: authenticated ? 'ok' : 'invalid_credentials', data: authenticated ? data : null } })
  })
  await page.route('**/console-api/logout', route => { authenticated = false; return route.fulfill({ json: { code: 'ok', data: null } }) })
}

test('session survives refresh without browser storage and logout clears the console', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await setup(page)
  await page.getByRole('link', { name: '运行总览', exact: true }).click()
  await expect(page.getByText('尚未就绪', { exact: true })).toHaveCount(2)
  await expect(page.getByText('正常', { exact: true })).toHaveCount(2)
  await page.screenshot({ path: testInfo.outputPath('overview-desktop.png'), fullPage: true })
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  await page.reload()
  await expect(page.getByRole('heading', { name: '运行总览' })).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await expect(page.getByRole('navigation')).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
})

test('grade pagination preserves snapshot in both directions and clears on filter change', async ({ page }) => {
  await setup(page)
  const requests: { body: any; headers: Record<string, string> }[] = []
  await page.route('**/console-api/query/api/**', async route => {
    requests.push({ body: route.request().postDataJSON(), headers: route.request().headers() })
    await route.fulfill({ headers: { 'X-Query-Snapshot': 'a'.repeat(64), 'X-Request-ID': 'test-query-id' }, json: { code: 'ok', message: 'OK', data: { total: 21, synced_at: '2026-09-28T10:00:00+08:00', items: [{ grade_id: 'g1', student: { student_number: '001', name: '<script>bad</script>' }, course: { course_code: 'M01', course_name: '数学' }, score: null, academic_year: '2025-2026', semester: '1' }] } } })
  })
  await openQueries(page)
  await page.getByLabel('学号 *', { exact: true }).fill('001')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByRole('cell', { name: '001 <script>bad</script>' })).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('2 / 2', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '上一页' }).click()
  await expect(page.getByText('1 / 2', { exact: true })).toBeVisible()
  expect(requests.map(r => r.body.page)).toEqual([1, 2, 1])
  expect(requests.map(r => r.headers['x-query-snapshot'])).toEqual([undefined, 'a'.repeat(64), 'a'.repeat(64)])
  expect(requests[0]!.headers['x-api-key']).toBeUndefined()
  expect(requests[0]!.headers['x-sicp-csrf']).toBe('synthetic-csrf')
  expect(requests[0]!.headers.authorization).toBeUndefined()
  await page.getByLabel('课程名称').fill('物理')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByText('1 / 2', { exact: true })).toBeVisible()
  expect(requests[3]!.headers['x-query-snapshot']).toBeUndefined()
})

test('overview shows initialization and automatically becomes ready when it finishes', async ({ page }, testInfo) => {
  await page.clock.install()
  await setup(page)
  const task = { task_id: 'initializing-task', mode: 'initialize', status: 'running', accepted_at: '2026-09-30T03:08:08Z', started_at: '2026-09-30T03:08:09Z' }
  let finished = false, checks = 0
  await page.route('**/admin/v1/tasks/', route => { checks++; return route.fulfill({ json: { code: 'ok', data: { initialized: finished, items: [{ ...task, status: finished ? 'success_published' : 'running' }] } } }) })
  await page.route('**/admin/v1/tasks/initializing-task', route => route.fulfill({ json: { code: 'ok', data: task } }))
  await page.route('**/console-api/**/readyz', route => route.fulfill({ status: finished ? 200 : 503, json: { ready: finished, dependencies_ready: true, query_ready: finished, sync_ready: finished } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  const progress = page.getByRole('region', { name: '正在初始化' })
  await expect(progress).toBeVisible()
  await expect(progress).toContainText('已运行')
  await expect(progress).toContainText('首次初始化完成前，业务数据暂不可查询。')
  await expect(page.getByRole('article').filter({ hasText: '数据更新服务' }).locator('.tag')).toHaveText('正在初始化')
  await page.screenshot({ path: testInfo.outputPath('initializing-overview-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('initializing-overview-mobile.png'), fullPage: true })
  finished = true
  await page.clock.runFor(10100)
  await expect(progress).toHaveCount(0)
  await expect(page.getByText('✓ 已就绪', { exact: true })).toHaveCount(2)
  await page.getByRole('button', { name: '打开主导航' }).click()
  await openQueries(page)
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  const beforeLeaving = checks
  await page.clock.runFor(20100)
  expect(checks).toBe(beforeLeaving)
  await page.getByRole('button', { name: '打开主导航' }).click()
  await page.getByRole('link', { name: '运行总览', exact: true }).click()
  await expect.poll(() => checks).toBeGreaterThan(beforeLeaving)
})

for (const entry of [
  { mode: 'incremental', status: 'running', title: '正在同步' },
  { mode: 'force_full', status: 'running', title: '正在全量更新' },
  { mode: 'incremental', status: 'accepted', title: '更新任务等待执行' },
]) {
  test(`overview distinguishes ${entry.title} from query availability`, async ({ page }) => {
    await setup(page)
    const task = { task_id: 'updating-task', ...entry, accepted_at: '2026-09-30T03:08:08Z' }
    await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: true, items: [task] } } }))
    await page.route('**/admin/v1/tasks/updating-task', route => route.fulfill({ json: { code: 'ok', data: task } }))
    await page.route('**/console-api/**/readyz', route => route.fulfill({ json: { ready: true, dependencies_ready: true } }))
    await page.getByRole('button', { name: '刷新状态', exact: true }).click()
    await expect(page.getByRole('region', { name: entry.title })).toContainText('当前已发布数据仍可查询。')
    await expect(page.getByRole('article').filter({ hasText: '业务查询服务' }).locator('.tag')).toHaveText('✓ 已就绪')
    await expect(page.getByRole('article').filter({ hasText: '数据更新服务' }).locator('.tag')).toHaveText(entry.title)
    await page.getByRole('link', { name: '查看更新任务 →', exact: true }).click()
    await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  })
}

test('overview does not keep reporting progress when task status cannot be read', async ({ page }) => {
  await setup(page)
  const task = { task_id: 'recovering-task', mode: 'initialize', status: 'running', accepted_at: '2026-09-30T03:08:08Z' }
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [task] } } }))
  await page.route('**/admin/v1/tasks/recovering-task', route => route.fulfill({ json: { code: 'ok', data: task } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByRole('region', { name: '正在初始化' })).toBeVisible()
  await page.route('**/console-api/worker/livez', route => route.fulfill({ status: 503, json: { live: false } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByText('更新服务状态异常或尚未确认，请结合下方检查结果判断任务是否仍在推进。')).toBeVisible()
  await expect(page.getByRole('article').filter({ hasText: '数据更新服务' }).locator('.tag')).toHaveText('尚未就绪')
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ status: 503, json: { code: 'unavailable' } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('无法确认更新任务状态')
  await expect(page.getByRole('region', { name: '正在初始化' })).toHaveCount(0)
})

test('unknown admission retries original operation with the same idempotency key', async ({ page }) => {
  await setup(page)
  const keys: string[] = []
  await page.route('**/admin/v1/tasks/force-full', async route => {
    const headers = route.request().headers()
    expect(headers.authorization).toBeUndefined()
    expect(headers['x-sicp-csrf']).toBe('synthetic-csrf')
    expect(headers['x-api-key']).toBeUndefined()
    keys.push(headers['idempotency-key']!)
    if (keys.length === 1) await route.abort()
    else await route.fulfill({ status: 202, json: { code: 'accepted', message: 'Task accepted', data: { task_id: 'task-001', status: 'accepted', accepted_at: '2026-09-28T10:00:00Z', created: false } } })
  })
  await page.route('**/admin/v1/tasks/task-001', route => route.fulfill({ json: { code: 'ok', data: { task_id: 'task-001', status: 'success_unchanged', mode: 'incremental', accepted_at: '2026-09-28T10:00:00Z', finished_at: '2026-09-28T10:01:00Z' } } }))
  await page.getByRole('link', { name: '更新事件' }).click()
  await page.getByRole('button', { name: '强制全量更新', exact: true }).click()
  await page.getByRole('button', { name: '确认提交更新' }).click()
  await expect(page.getByRole('alert')).toContainText('原请求已保留')
  await expect(page.getByLabel('幂等键', { exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '重试原请求' }).click()
  await page.getByRole('button', { name: '确认提交更新' }).click()
  await expect(page.getByText('检查完成，无变化', { exact: true })).toBeVisible()
  expect(keys).toHaveLength(2)
  expect(keys[1]).toBe(keys[0])
  await expect(page.getByLabel('幂等键', { exact: true })).not.toHaveValue(keys[0]!)
  await expect(page.getByRole('checkbox')).toBeDisabled()
})

test('abandoning an unconfirmed task renews the idempotency key', async ({ page }) => {
  await setup(page)
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  const key = page.getByLabel('幂等键', { exact: true })
  const original = await key.inputValue()
  await page.getByRole('button', { name: '强制全量更新', exact: true }).click()
  await page.getByRole('button', { name: '暂不提交', exact: true }).click()
  await page.getByRole('button', { name: '结束本次请求', exact: true }).click()
  await expect(key).toBeEnabled()
  await expect(key).toHaveValue(/^[a-f0-9]{32}$/)
  await expect(key).not.toHaveValue(original)
})

test('failed initialization exposes a retry entry without enabling duplicate bootstrap', async ({ page }, testInfo) => {
  await setup(page)
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, interval_seconds: 7200, items: [{ task_id: 'automatic-initial-task', mode: 'initialize', status: 'failed', operator: 'scheduler', accepted_at: '2026-09-28T10:00:00Z' }] } } }))
  await page.route('**/admin/v1/tasks/automatic-initial-task', route => route.fulfill({ json: { code: 'ok', data: { task_id: 'automatic-initial-task', mode: 'initialize', status: 'failed', operator: 'scheduler', accepted_at: '2026-09-28T10:00:00Z', failure: 'acquisition_failed' } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByRole('button', { name: '重新初始化', exact: true })).toBeEnabled()
  await expect(page.getByText('初始化未完成，可重新采集待补齐的表，或选择单表强制更新；已完成的表会保留复用，上游冷却期间任务会等待。')).toBeVisible()
  await expect(page.getByText(/重启后系统会恢复未完成的初始化/)).toBeVisible()
  await expect(page.getByRole('radio')).toHaveCount(4)
  await expect(page.getByRole('radio', { name: /首次初始化/ })).toHaveCount(0)
  await expect(page.getByRole('radio', { name: /单表强制更新/ })).toBeVisible()
  await expect(page.getByRole('button', { name: '立即增量同步', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: /automatic-initial-task/ }).click()
  await expect(page.getByText('失败原因：acquisition_failed')).toBeVisible()
  await expect(page.getByText('系统调度', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('automatic-tasks-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('automatic-tasks-mobile.png'), fullPage: true })
})

test('initialization retry keeps its predecessor and key across lost replies and history refresh', async ({ page }) => {
  await setup(page)
  const original = { task_id: 'failed-init', mode: 'initialize', status: 'failed', operator: 'scheduler', accepted_at: '2026-09-28T10:00:00Z' }
  let latest = original
  const keys: string[] = []
  let forceCalls = 0
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [latest] } } }))
  await page.route('**/admin/v1/tasks/force-full', route => { forceCalls++; return route.fulfill({ status: 409, json: { code: 'initialize_required' } }) })
  await page.route('**/admin/v1/tasks/failed-init/rerun', route => {
    expect(route.request().method()).toBe('POST')
    keys.push(route.request().headers()['idempotency-key']!)
    if (keys.length === 1) return route.abort()
    latest = { ...original, task_id: 'retry-init', status: 'accepted' }
    return route.fulfill({ status: 202, json: { code: 'accepted', data: { task_id: 'retry-init', status: 'accepted' } } })
  })
  await page.route('**/admin/v1/tasks/retry-init', route => route.fulfill({ json: { code: 'ok', data: { ...latest, predecessor_id: 'failed-init' } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await page.getByRole('button', { name: '重新初始化', exact: true }).click()
  await expect(page.getByRole('heading', { name: '确认重新初始化', exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toContainText('failed-init')
  await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('原请求已保留')
  latest = { ...original, task_id: 'another-failed-init' }
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(page.getByRole('button', { name: /another-failed-init/ })).toBeVisible()
  await page.getByRole('button', { name: '重试原请求', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('failed-init')
  await page.getByRole('button', { name: '确认提交更新', exact: true }).click()
  await expect(page.getByText('任务已受理：retry-init。受理不代表执行完成。')).toBeVisible()
  expect(keys).toHaveLength(2)
  expect(keys[0]).toBe(keys[1])
  expect(forceCalls).toBe(0)
  await expect(page.getByRole('button', { name: '强制全量更新', exact: true })).toBeDisabled()
})

test('update entry explains active work and blocks use of stale history', async ({ page }) => {
  await setup(page)
  let status = 'running', unavailable = false
  const task = { task_id: 'initial-task', mode: 'initialize', accepted_at: '2026-09-28T10:00:00Z' }
  await page.route('**/admin/v1/tasks/', route => unavailable
    ? route.fulfill({ status: 503, json: { code: 'unavailable' } })
    : route.fulfill({ json: { code: 'ok', data: { initialized: false, items: [{ ...task, status }] } } }))
  await page.route('**/admin/v1/tasks/initial-task', route => route.fulfill({ json: { code: 'ok', data: { ...task, status } } }))
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByRole('button', { name: '强制全量更新', exact: true })).toBeDisabled()
  await expect(page.getByText('已有更新任务正在执行或等待恢复，完成后才能再次启动。')).toBeVisible()
  status = 'failed'
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(page.getByRole('button', { name: '重新初始化', exact: true })).toBeEnabled()
  unavailable = true
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  await expect(page.locator('#force-full-help')).toHaveText('无法确认当前任务状态，请先刷新记录。')
  await expect(page.locator('#incremental-help')).toHaveText('无法确认当前任务状态，请先刷新记录。')
  await expect(page.getByRole('button', { name: '重新初始化', exact: true })).toBeDisabled()
})

test('query failures display request ID and mobile pages fit viewport', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setup(page)
  await page.route('**/console-api/query/api/**', route => route.fulfill({ status: 503, json: { code: 'unavailable', message: 'Authentication required', request_id: 'denied-id', data: null } }))
  await page.getByRole('button', { name: '打开主导航' }).click()
  await openQueries(page)
  await page.getByLabel('学号 *', { exact: true }).fill('001')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByRole('alert')).toContainText('denied-id')
  await page.screenshot({ path: testInfo.outputPath('query-mobile.png'), fullPage: true })
  for (const title of ['运行总览', '接口中心', '更新事件']) {
    await page.getByRole('button', { name: '打开主导航' }).click()
    await page.getByRole('link', { name: title, exact: true }).click()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  }
})

for (const entry of [
  { title: '批量成绩', path: '/students/grades/batch-query', identity: '学号列表（可选）', value: '001,002', expected: { student_numbers: ['001', '002'], academic_year: '2025-2026', semester: '1', page: 1, page_size: 20 } },
  { title: '学生课表', path: '/students/timetable/query', identity: '学号 *', value: '001', expected: { student_number: '001', academic_year: '2025-2026', semester: '1' } },
  { title: '教师课表', path: '/staff/timetable/query', identity: '工号 *', value: 'T001', expected: { staff_number: 'T001', academic_year: '2025-2026', semester: '1' } },
]) {
  test(`${entry.title} sends its own contract and no snapshot`, async ({ page }) => {
    await setup(page)
    let payload: unknown
    await page.route(`**/console-api/query/api/v1/sicp${entry.path}`, async route => {
      payload = route.request().postDataJSON()
      expect(route.request().headers()['x-query-snapshot']).toBeUndefined()
      await route.fulfill({ json: { code: 'ok', data: { items: [], total: 0, synced_at: '2026-09-28T10:00:00Z' } } })
    })
    await openQueries(page)
    await page.getByRole('button', { name: entry.title, exact: true }).click()
    await page.getByLabel(entry.identity, { exact: true }).fill(entry.value)
    await page.getByLabel('学年 *', { exact: true }).fill('2025-2026')
    await page.getByLabel(/^学期/).selectOption('1')
    await page.getByRole('button', { name: '查询数据' }).click()
    await expect(page.getByText('没有符合条件的数据，请调整筛选条件后重新查询。')).toBeVisible()
    expect(payload).toEqual(entry.expected)
  })
}

for (const returnBeforeResponse of [false, true]) {
  test(`overview ignores a late response after leaving (returned=${returnBeforeResponse})`, async ({ page }) => {
    await setup(page)
    await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
    let release!: () => void, entered!: () => void, pendingRequest: import('@playwright/test').Request
    const gate = new Promise<void>(resolve => { release = resolve })
    const seen = new Promise<void>(resolve => { entered = resolve })
    let lists = 0, obsoleteDetails = 0
    await page.route('**/admin/v1/tasks/', async route => {
      const first = ++lists === 1
      if (first) { pendingRequest = route.request(); entered(); await gate }
      await route.fulfill({ json: { code: 'ok', data: { initialized: true, items: first ? [{ task_id: 'obsolete-overview', mode: 'initialize', status: 'running' }] : [] } } })
    })
    await page.route('**/admin/v1/tasks/obsolete-overview', route => {
      obsoleteDetails++
      return route.fulfill({ json: { code: 'ok', data: { task_id: 'obsolete-overview', mode: 'initialize', status: 'running' } } })
    })
    try {
      await page.getByRole('button', { name: '刷新状态', exact: true }).click()
      await seen
      await openQueries(page)
      await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
      if (returnBeforeResponse) {
        await page.getByRole('link', { name: '运行总览', exact: true }).click()
        await expect.poll(() => lists).toBe(2)
        await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
      }
      release()
      await expect.poll(() => pendingRequest.failure()).not.toBeNull()
      await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
      expect(obsoleteDetails).toBe(0)
      if (!returnBeforeResponse) await page.getByRole('link', { name: '运行总览', exact: true }).click()
      await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
      await expect(page.getByRole('region', { name: '正在初始化' })).toHaveCount(0)
    } finally { release() }
  })
}

test('task page resumes while an old detail request is still pending', async ({ page }) => {
  await setup(page)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  const tasks = ['slow-old-task', 'selected-new-task'].map(task_id => ({ task_id, mode: 'incremental', status: 'success_published', accepted_at: '2026-10-08T00:00:00Z' }))
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: true, interval_seconds: 7200, items: tasks } } }))
  let release!: () => void, entered!: () => void, pendingRequest: import('@playwright/test').Request
  const gate = new Promise<void>(resolve => { release = resolve })
  const seen = new Promise<void>(resolve => { entered = resolve })
  let oldCalls = 0
  await page.route('**/admin/v1/tasks/slow-old-task', async route => {
    if (++oldCalls === 1) { pendingRequest = route.request(); entered(); await gate }
    await route.fulfill({ json: { code: 'ok', data: tasks[0] } })
  })
  await page.route('**/admin/v1/tasks/selected-new-task', route => route.fulfill({ json: { code: 'ok', data: tasks[1] } }))
  try {
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await page.getByRole('button', { name: /slow-old-task/ }).click()
    await seen
    await openQueries(page)
    await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await page.getByRole('button', { name: /selected-new-task/ }).click()
    await expect(page.locator('.task-detail .task-id')).toHaveText('selected-new-task')
    release()
    await expect.poll(() => pendingRequest.failure()).not.toBeNull()
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
    await expect(page.locator('.task-detail .task-id')).toHaveText('selected-new-task')
    await expect(page.getByLabel('查找任务')).toHaveValue('selected-new-task')
  } finally { release() }
})

const phase2Queries = [
  { title: '组织查询', path: '/organizations/query', fields: {}, advanced: { is_active: false }, expected: { is_active: false },
    row: { organization_code: '10003', organization_name: '停用单位', parent_organization_code: null, category_name: null, is_active: false }, cell: '停用或未知' },
  { title: '学生信息', path: '/students/query', fields: { '学号（可选）': '001' }, advanced: { study_years: 3, status_codes: ['unknown'] }, expected: { student_number: '001', study_years: 3, status_codes: ['unknown'] },
    row: { student_number: '001', name: '<script>synthetic</script>', department: null, class_name: null, major_name: null, email: null, mobile: 'raw;value' }, cell: '<script>synthetic</script>' },
  { title: '教职工信息', path: '/staff/query', fields: { '工号（可选）': 'T001' }, advanced: { status_codes: ['unknown'] }, expected: { staff_number: 'T001', status_codes: ['unknown'] },
    row: { staff_number: 'T001', name: '合成教师', department: { organization_code: 'D001', organization_name: null }, status_code: 'unknown', email: null, mobile: 'raw;value' }, cell: 'D001' },
  { title: '教师教学班', path: '/staff/teaching-classes/query', fields: { '工号 *': 'T001', '学年 *': '2026-2027' }, semester: true,
    expected: { staff_number: 'T001', academic_year: '2026-2027', semester: '1' },
    row: { teaching_class_number: 'J001', teaching_class_name: null, planned_course_code: 'C001', course_name: null, teachers: [{ staff_number: 'T001', name: null }], student_count: 0 }, cell: '0' },
  { title: '教学班学生', path: '/teaching-classes/students/query', fields: { '教学班编号 *': 'J001', '计划课程编号 *': 'C001', '学年 *': '2026-2027' }, semester: true,
    expected: { teaching_class_number: 'J001', planned_course_code: 'C001', academic_year: '2026-2027', semester: '1' },
    row: { student_number: 'MISSING', name: null, department_code: null, department_name: null, class_code: null, class_name: null }, cell: 'MISSING' },
]

for (const entry of phase2Queries) {
  test(`T25-UI ${entry.title} contract, null fields, pagination and mobile`, async ({ page }, testInfo) => {
    await setup(page)
    const requests: { body: any; headers: Record<string, string> }[] = []
    let fail = false
    await page.route(`**/console-api/query/api/v1/sicp${entry.path}`, async route => {
      requests.push({ body: route.request().postDataJSON(), headers: route.request().headers() })
      if (fail) return route.fulfill({ status: 409, json: { code: 'conflict', request_id: 'phase2-conflict', data: null } })
      return route.fulfill({ headers: { 'X-Query-Snapshot': 'b'.repeat(64) }, json: { code: 'ok', data: { total: 21, synced_at: '2026-10-12T08:00:00Z', items: [entry.row] } } })
    })
    await openQueries(page)
    await expect(page.locator('[aria-label="查询接口"] button')).toHaveCount(9)
    await page.getByRole('button', { name: entry.title, exact: true }).click()
    for (const [label, value] of Object.entries(entry.fields)) await page.getByLabel(label, { exact: true }).fill(value!)
    if (entry.semester) await page.getByRole('combobox', { name: '学期 *', exact: true }).selectOption('1')
    else await expect(page.getByLabel(/^学年/)).toHaveCount(0)
    if (entry.advanced) {
      await page.getByText('高级筛选', { exact: true }).click()
      await page.getByLabel('高级筛选 JSON').fill(JSON.stringify(entry.advanced))
    }
    await page.getByRole('button', { name: '查询数据', exact: true }).click()
    await expect(page.getByRole('cell', { name: entry.cell, exact: true })).toBeVisible()
    expect(requests[0]!.body).toEqual({ ...entry.expected, page: 1, page_size: 20 })
    expect(requests[0]!.headers['x-api-key']).toBeUndefined()
    expect(requests[0]!.headers['x-sicp-csrf']).toBe('synthetic-csrf')
    await page.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(page.getByText('2 / 2', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '上一页', exact: true }).click()
    await expect(page.getByText('1 / 2', { exact: true })).toBeVisible()
    expect(requests.map(request => request.body.page)).toEqual([1, 2, 1])
    expect(requests.map(request => request.headers['x-query-snapshot'])).toEqual([undefined, 'b'.repeat(64), 'b'.repeat(64)])
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
    await page.screenshot({ path: testInfo.outputPath(`${entry.path.replaceAll('/', '-')}-mobile.png`), fullPage: true })
    fail = true
    await page.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('phase2-conflict')
    await expect(page.getByRole('table')).toHaveCount(0)
    fail = false
    await page.getByRole('button', { name: '查询数据', exact: true }).click()
    await expect(page.getByText('1 / 2', { exact: true })).toBeVisible()
    expect(requests.at(-1)!.headers['x-query-snapshot']).toBeUndefined()
  })
}

test('T25-UI directory matches backend and filters retain false, zero and identifiers', () => {
  const manifest = JSON.parse(readFileSync(new URL('../../development_docs/examples/phase2_delivery.json', import.meta.url), 'utf8'))
  const actual = operations.map(op => `${op.operation} ${op.path}`).sort()
  expect(actual).toEqual(manifest.operations.map((op: { operation: string; path: string }) => `${op.operation} ${op.path}`).sort())
  for (const op of operations) expect(op.paginated).toBe(manifest.operations.find((item: { operation: string }) => item.operation === op.operation).snapshot)
  const org = operations.find(op => op.kind === 'organizations')!
  expect(queryBody(org, {}, '{"is_active":false}', 1000)).toEqual({ is_active: false, page: 1, page_size: 1000 })
  expect(() => queryBody(org, {}, '{"staff_number":"not-allowed"}', 20)).toThrow()
  expect(() => queryBody(operations.find(op => op.kind === 'members')!, {}, '{}', 20)).toThrow()
  expect(cell({ student_count: 0 }, 'student_count')).toBe(0)
  expect(cell({ department: null }, 'department.organization_code')).toBe('未提供')
})

test('T25-UI switching query clears incompatible fields, results and snapshot', async ({ page }) => {
  await setup(page)
  const requests: { path: string; body: any; snapshot?: string }[] = []
  await page.route('**/console-api/query/api/v1/sicp/**', route => {
    requests.push({ path: new URL(route.request().url()).pathname, body: route.request().postDataJSON(), snapshot: route.request().headers()['x-query-snapshot'] })
    return route.fulfill({ headers: { 'X-Query-Snapshot': 'c'.repeat(64) }, json: { code: 'ok', data: { total: 0, synced_at: '2026-10-12T08:00:00Z', items: [] } } })
  })
  await openQueries(page)
  await page.getByRole('button', { name: '教师教学班', exact: true }).click()
  await page.getByLabel('工号 *', { exact: true }).fill('T001')
  await page.getByLabel('学年 *', { exact: true }).fill('2026-2027')
  await page.getByRole('combobox', { name: '学期 *', exact: true }).selectOption('1')
  await page.getByRole('button', { name: '查询数据', exact: true }).click()
  await expect(page.getByText('没有符合条件的数据，请调整筛选条件后重新查询。')).toBeVisible()
  await page.getByRole('button', { name: '教职工信息', exact: true }).click()
  await expect(page.getByLabel('工号（可选）', { exact: true })).toHaveValue('')
  await page.getByRole('button', { name: '查询数据', exact: true }).click()
  await expect(page.getByText('没有符合条件的数据，请调整筛选条件后重新查询。')).toBeVisible()
  expect(requests[1]!.body).toEqual({ page: 1, page_size: 20 })
  expect(requests[1]!.snapshot).toBeUndefined()
})

test('F01 lazy routes preserve deep links, history and old hashes without executing queries', async ({ page }) => {
  await setup(page)
  let queries = 0
  await page.route('**/console-api/query/api/**', route => { queries++; return route.abort() })
  await openQueries(page)
  await expect(page).toHaveURL(/\/console\/apis$/)
  await page.reload()
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/updates$/)
  await page.goBack()
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  await page.goto('./#tasks')
  await expect(page).toHaveURL(/\/console\/updates$/)
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  expect(queries).toBe(0)
})

test('F01 a deactivated query ignores late original-mock results and can query again', async ({ page }) => {
  await setup(page)
  const fixture = backendCapture('students.query')
  let release!: () => void, entered!: () => void, pendingRequest: import('@playwright/test').Request
  const gate = new Promise<void>(resolve => { release = resolve })
  const seen = new Promise<void>(resolve => { entered = resolve })
  let calls = 0
  await page.route('**/console-api/query/api/v1/sicp/students/query', async route => {
    expect(route.request().postDataJSON()).toEqual({ ...fixture.request, page: 1, page_size: 20 })
    if (++calls === 1) { pendingRequest = route.request(); entered(); await gate }
    await route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response })
  })
  try {
    await openQueries(page)
    await page.getByRole('button', { name: '学生信息', exact: true }).click()
    await page.getByRole('button', { name: '查询数据', exact: true }).click()
    await seen
    await page.getByRole('link', { name: '运行总览', exact: true }).click()
    release()
    await expect.poll(() => pendingRequest.failure()).not.toBeNull()
    await openQueries(page)
    await expect(page.getByRole('table')).toHaveCount(0)
    await page.getByRole('button', { name: '查询数据', exact: true }).click()
    await expect(page.getByRole('table')).toContainText('00202600001')
    await expect(page.getByRole('table')).toContainText('虚构学生001')
    await expect(page.getByRole('row')).toHaveCount(13)
  } finally { release() }
})

test('F01 leaving an in-flight submission preserves the original operation and key', async ({ page }) => {
  await setup(page)
  const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
  const receipt = examples.find((item: { name: string }) => item.name === 'full')
  const detail = examples.find((item: { name: string }) => item.name === 'task_detail')
  let entered!: () => void, release!: () => void, pendingRequest: import('@playwright/test').Request
  const seen = new Promise<void>(resolve => { entered = resolve })
  const gate = new Promise<void>(resolve => { release = resolve })
  const keys: string[] = []
  await page.route('**/admin/v1/tasks/force-full', async route => {
    keys.push(route.request().headers()['idempotency-key']!)
    expect(route.request().postDataJSON()).toEqual(receipt.request)
    if (keys.length === 1) { pendingRequest = route.request(); entered(); await gate }
    await route.fulfill({ status: receipt.status, json: receipt.response })
  })
  await page.route(`**${detail.path}`, route => route.fulfill({ status: detail.status, json: detail.response }))
  try {
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await page.getByRole('button', { name: '强制全量更新', exact: true }).click()
    await page.getByRole('button', { name: '确认提交更新' }).click()
    await seen
    await openQueries(page)
    await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
    release()
    await expect.poll(() => pendingRequest.failure()).not.toBeNull()
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('受理结果可能未知')
    await page.getByRole('button', { name: '重试原请求' }).click()
    await page.getByRole('button', { name: '确认提交更新' }).click()
    await expect(page.locator('.success-text')).toContainText(receipt.response.data.task_id)
    expect(keys).toHaveLength(2)
    expect(keys[1]).toBe(keys[0])
  } finally { release() }
})

for (const outcome of ['retry', 'logout']) {
  test(`F01 pending operation survives refresh and clears on ${outcome}`, async ({ page }) => {
    await setup(page)
    const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
    const receipt = examples.find((item: { name: string }) => item.name === 'full')
    const detail = examples.find((item: { name: string }) => item.name === 'task_detail')
    const keys: string[] = []
    await page.route('**/admin/v1/tasks/force-full', route => {
      keys.push(route.request().headers()['idempotency-key']!)
      if (keys.length === 1) return route.abort()
      return route.fulfill({ status: receipt.status, headers: { Location: detail.path }, json: receipt.response })
    })
    await page.route(`**${detail.path}`, route => route.fulfill({ status: detail.status, json: detail.response }))
    await page.getByRole('link', { name: '更新事件', exact: true }).click()
    await page.getByRole('button', { name: '强制全量更新', exact: true }).click()
    await page.getByRole('button', { name: '确认提交更新' }).click()
    await expect(page.getByRole('alert')).toContainText('原请求已保留')
    const stored = await page.evaluate(() => JSON.parse(sessionStorage.getItem('sicp.pending-operations.v1')!))
    expect(stored).toHaveLength(1)
    expect(stored[0].operation).toEqual({ kind: 'task', path: receipt.path, key: keys[0], payload: '{}' })
    expect(JSON.stringify(stored)).not.toMatch(/csrf|password|synthetic-csrf|synthetic-password/)
    await page.reload()
    await expect(page.getByRole('alert')).toContainText('已恢复待确认操作')
    await expect(page.getByLabel('幂等键', { exact: true })).toHaveValue(keys[0]!)
    expect(keys).toHaveLength(1)
    if (outcome === 'retry') {
      await page.getByRole('button', { name: '重试原请求' }).click()
      await page.getByRole('button', { name: '确认提交更新' }).click()
      await expect(page.locator('.success-text')).toContainText(receipt.response.data.task_id)
      expect(keys).toHaveLength(2)
      expect(keys[1]).toBe(keys[0])
    } else {
      await page.getByRole('button', { name: '退出登录' }).click()
      await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
    }
    expect(await page.evaluate(() => sessionStorage.getItem('sicp.pending-operations.v1'))).toBeNull()
  })
}

test('F01 read polling backs off, pauses when hidden and resumes without posting', async ({ page }) => {
  await page.clock.install()
  await setup(page)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  let reads = 0, posts = 0, failing = true
  page.on('request', request => { if (request.method() === 'POST') posts++ })
  await page.route('**/admin/v1/tasks/', route => {
    reads++
    return route.fulfill(failing ? { status: 503, json: { code: 'unavailable', data: null } } : { json: { code: 'ok', data: { initialized: true, items: [] } } })
  })
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('无法确认更新任务状态')
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  expect(reads).toBe(1)
  await page.clock.runFor(19000)
  expect(reads).toBe(1)
  await page.clock.runFor(1500)
  await expect.poll(() => reads).toBe(2)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  await page.clock.runFor(38000)
  expect(reads).toBe(2)
  await page.clock.runFor(2500)
  await expect.poll(() => reads).toBe(3)
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.clock.runFor(120000)
  expect(reads).toBe(3)
  failing = false
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => reads).toBe(4)
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  await page.clock.runFor(10500)
  await expect.poll(() => reads).toBe(5)
  expect(posts).toBe(0)
})

test('F02 desktop shell uses planned dimensions and collapsed navigation stays usable', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await setup(page)
  await expect(page.getByRole('heading', { name: '运行总览', exact: true })).toBeVisible()
  const dimensions = () => page.evaluate(() => ({
    sidebar: document.querySelector('.sidebar')!.getBoundingClientRect().width,
    topbar: document.querySelector('.topbar')!.getBoundingClientRect().height,
    padding: getComputedStyle(document.querySelector('#main')!).paddingLeft,
    title: getComputedStyle(document.querySelector('h1')!).fontSize,
    overflow: document.documentElement.scrollWidth > innerWidth,
  }))
  expect(await dimensions()).toEqual({ sidebar: 216, topbar: 56, padding: '24px', title: '24px', overflow: false })
  await page.screenshot({ path: testInfo.outputPath('shell-expanded.png'), fullPage: true })
  await page.getByRole('button', { name: '折叠侧栏', exact: true }).click()
  await expect(page.getByRole('button', { name: '展开侧栏', exact: true })).toHaveAttribute('aria-expanded', 'false')
  expect((await dimensions()).sidebar).toBe(64)
  await page.screenshot({ path: testInfo.outputPath('shell-collapsed.png'), fullPage: true })
  const queryLink = page.getByRole('link', { name: '接口中心', exact: true })
  await queryLink.focus()
  await page.keyboard.press('Enter')
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  const fixture = backendCapture('students.query')
  await page.route('**/console-api/query/api/v1/sicp/students/query', route => route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }))
  await page.getByRole('button', { name: '学生信息', exact: true }).click()
  await page.getByRole('button', { name: '查询数据', exact: true }).click()
  await expect(page.getByRole('table')).toContainText('虚构学生001')
  expect(await page.getByRole('row').first().evaluate(row => row.getBoundingClientRect().height)).toBe(44)
  await page.getByRole('button', { name: '展开侧栏', exact: true }).click()
  expect((await dimensions()).sidebar).toBe(216)
  await page.screenshot({ path: testInfo.outputPath('shell-table.png'), fullPage: true })
})

test('F02 responsive shell uses three breakpoints and mobile navigation drawer without extra queries', async ({ page }, testInfo) => {
  await setup(page)
  let queries = 0
  const fixture = backendCapture('students.query')
  await page.route('**/console-api/query/api/**', route => { queries++; return route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }) })
  for (const width of [1440, 1280, 1279, 1024, 768, 767, 390]) {
    await page.setViewportSize({ width, height: 900 })
    const state = await page.evaluate(() => ({ sidebar: document.querySelector('.sidebar')!.getBoundingClientRect().width, overflow: document.documentElement.scrollWidth > innerWidth }))
    expect(state).toEqual({ sidebar: width >= 1280 ? 216 : width >= 768 ? 64 : 0, overflow: false })
    if (width >= 768) await expect(page.getByRole('link', { name: '接口中心', exact: true })).toHaveAttribute('title', '接口中心')
    if ([1440, 1024, 768, 390].includes(width)) await page.screenshot({ path: testInfo.outputPath(`responsive-${width}.png`), fullPage: true })
  }
  const menu = page.getByRole('button', { name: '打开主导航' })
  await menu.click()
  const navigation = page.getByRole('dialog', { name: '主导航', exact: true })
  await expect(navigation).toBeVisible()
  await expect(page.locator('.mobile-navigation-toggle')).toHaveAttribute('aria-expanded', 'true')
  await page.screenshot({ path: testInfo.outputPath('mobile-navigation.png') })
  await page.keyboard.press('Escape')
  await expect(navigation).not.toBeVisible()
  await expect(menu).toBeFocused()
  await menu.click()
  await navigation.getByRole('link', { name: '接口中心', exact: true }).click()
  await expect(page.getByRole('heading', { name: '接口中心', exact: true })).toBeVisible()
  await expect(page.locator('#main')).toBeFocused()
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  await expect(navigation).not.toBeVisible()
  expect(queries).toBe(0)
  await page.getByRole('button', { name: '学生信息', exact: true }).click()
  const inputs = page.locator('.form-grid input')
  const boxes = await inputs.evaluateAll(elements => elements.map(element => element.getBoundingClientRect().left))
  expect(new Set(boxes).size).toBe(1)
  const submit = page.getByRole('button', { name: '查询数据', exact: true })
  expect(await submit.evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThanOrEqual(44)
  await submit.click()
  await expect(page.getByRole('table')).toContainText('虚构学生001')
  const overflow = await page.locator('.table-wrap').evaluate(element => ({ inside: element.scrollWidth > element.clientWidth, page: document.documentElement.scrollWidth > innerWidth }))
  expect(overflow).toEqual({ inside: true, page: false })
  await page.screenshot({ path: testInfo.outputPath('mobile-query-table.png'), fullPage: true })
  expect(queries).toBe(1)
  await menu.click()
  await page.setViewportSize({ width: 1024, height: 900 })
  await expect(navigation).not.toBeVisible()
  expect(await page.evaluate(() => document.documentElement.style.overflow)).toBe('')
})

test('F03 login distinguishes throttling and availability and returns to the original internal target', async ({ page }) => {
  await page.route('**/console-api/**', route => route.abort())
  await page.route('**/admin/v1/**', route => route.abort())
  let authenticated = false, mode: 'limited' | 'proxy' | 'offline' | 'success' = 'limited', attempts = 0, queries = 0
  const auth = { operator: 'admin', csrf: 'synthetic-csrf' }
  await page.route('**/console-api/session', route => route.fulfill({ status: authenticated ? 200 : 401, json: { code: authenticated ? 'ok' : 'unauthenticated', data: authenticated ? auth : null } }))
  await page.route('**/console-api/login', route => {
    attempts++
    if (mode === 'offline') return route.abort('failed')
    if (mode === 'limited') return route.fulfill({ status: 429, headers: { 'Retry-After': '60' }, json: { code: 'rate_limited', data: null } })
    if (mode === 'proxy') return route.fulfill({ status: 503, contentType: 'text/html', body: '<h1>Unavailable</h1>' })
    authenticated = true
    return route.fulfill({ json: { code: 'ok', data: auth } })
  })
  await page.route('**/console-api/query/api/**', route => { queries++; return route.abort() })
  const target = '/console/queries?redirect=https%3A%2F%2Fexample.invalid'
  await page.goto(target)
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await page.getByLabel('管理员账号').fill('admin')
  const password = page.getByLabel('密码', { exact: true })
  const login = page.getByRole('button', { name: '登录', exact: true })
  for (const entry of [ { mode: 'limited', text: '60 秒' }, { mode: 'proxy', text: 'HTTP 503' }, { mode: 'offline', text: '无法连接登录服务' } ] as const) {
    mode = entry.mode
    await password.fill('synthetic-password')
    await login.click()
    await expect(page.getByRole('alert')).toContainText(entry.text)
    await expect(password).toHaveValue('')
    await expect(password).toHaveAttribute('aria-invalid', 'false')
  }
  expect(attempts).toBe(3)
  mode = 'success'
  await password.fill('synthetic-password')
  await login.click()
  await showQueries(page)
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  expect(new URL(page.url()).pathname).toBe('/console/apis')
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('https://example.invalid')
  expect(attempts).toBe(4); expect(queries).toBe(0)
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
})

test('F03 primary navigation has seven modules with no fabricated module results', async ({ page }, testInfo) => {
  await setup(page)
  const titles = ['运行总览', '接口中心', '更新事件', '数据源', '日志中心', '通知中心', '系统设置']
  const nav = page.getByRole('navigation', { name: '主导航', exact: true })
  await expect(nav.getByRole('link')).toHaveCount(7)
  expect(await nav.getByRole('link').allTextContents()).toEqual(titles.map((title, i) => `${['◫', '⌕', '↻', '▤', '≡', '♧', '⚙'][i]}${title}`))
  await expect(nav.getByRole('link', { name: '业务查询', exact: true })).toHaveCount(0)
  let requests = 0
  await page.route('**/admin/v1/**', route => { requests++; return route.abort() })
  for (const title of titles.slice(4)) {
    await nav.getByRole('link', { name: title, exact: true }).click()
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: '暂未开放', exact: true })).toBeVisible()
    await expect(page.getByRole('table')).toHaveCount(0)
  }
  const afterNavigation = requests
  await page.waitForTimeout(200)
  expect(requests).toBe(afterNavigation)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: '打开主导航' }).click()
  await expect(page.getByRole('navigation', { name: '主导航' }).getByRole('link')).toHaveCount(7)
  await page.screenshot({ path: testInfo.outputPath('seven-module-navigation.png') })
})

test('F03 route skeleton resolves details, 403 and 404 and rejects external login return targets', async ({ page }) => {
  await setup(page)
  const targets = [
    ['/apis/students.query', '学生信息', '接口中心'], ['/updates/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', '更新详情', '更新事件'],
    ['/sources', '数据源', '数据源'], ['/logs/platform', '日志中心', '日志中心'],
    ['/notifications', '通知中心', '通知中心'], ['/settings/retention', '系统设置', '系统设置'],
  ]
  for (const [path, title, navigation] of targets) {
    await page.goto(`/console${path}`)
    await expect(page.getByRole('heading', { name: title!, exact: true })).toBeVisible()
    await expect(page.getByRole('navigation').getByRole('link', { name: navigation!, exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.locator('.topbar-location')).toContainText(navigation!)
  }
  for (const [path, title] of [['403', '无权访问'], ['404', '页面不存在'], ['not-a-console-page', '页面不存在']]) {
    await page.goto(`/console/${path}`)
    await expect(page.getByRole('heading', { name: title!, exact: true })).toBeVisible()
  }
  await page.goto('/console/login?returnTo=https%3A%2F%2Fexample.invalid')
  await expect(page).toHaveURL(/\/console\/overview$/)
  await page.goto('/console/login?returnTo=%2F%2Fexample.invalid')
  await expect(page).toHaveURL(/\/console\/overview$/)
  await page.goto('/console/login?returnTo=%2Fapis%2Fstudents.query')
  await expect(page).toHaveURL(/\/console\/apis\/students.query$/)
  await expect(page.getByRole('heading', { name: '学生信息', exact: true })).toBeVisible()
})

test('F03 log detail keeps source and backend event ID while API tests have no standalone route', async ({ page }) => {
  const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
  const record = examples.find((item: { name: string }) => item.name === 'log_detail').response.data
  const { logDetailPath } = await import('../src/app/links')
  const path = logDetailPath(record.source, record.event_id)
  expect(path).toBe(`/logs/${record.source}/events/${record.event_id}`)
  expect(logDetailPath(record.source, 'raw-request-id')).toBeNull()
  expect(logDetailPath('unknown', record.event_id)).toBeNull()
  await setup(page)
  await page.goto(`/console${path}`)
  await expect(page.getByRole('heading', { name: '日志详情', exact: true })).toBeVisible()
  await expect(page.getByRole('navigation').getByRole('link', { name: '日志中心', exact: true })).toHaveAttribute('aria-current', 'page')
  expect(new URL(page.url()).pathname).toBe(`/console${path}`)
  for (const target of ['/apis/students.query/test', '/api-tests', `/logs/unknown/events/${record.event_id}`, '/logs/platform/events/raw-request-id']) {
    await page.goto(`/console${target}`)
    await expect(page.getByRole('heading', { name: '页面不存在', exact: true })).toBeVisible()
  }
})

test('F03 legacy hash and task links preserve destinations without queries or writes', async ({ page }) => {
  await setup(page)
  let writes = 0, queries = 0
  await page.route('**/console-api/query/api/**', route => { queries++; return route.abort() })
  page.on('request', request => { if (request.url().includes('/admin/') && request.method() !== 'GET') writes++ })
  const examples = JSON.parse(readFileSync(new URL('../../development_docs/verification/operations_http_examples.json', import.meta.url), 'utf8')).examples
  const taskId = examples.find((item: { name: string }) => item.name === 'task_detail').response.data.task_id
  for (const [oldPath, expected] of [
    ['/console/#overview', '/console/overview'], ['/console/#queries', '/console/apis'], ['/console/#tasks', '/console/updates'],
    [`/console/sync/tasks/${taskId}`, `/console/updates/${taskId}`], [`/console/tasks/${taskId}`, `/console/updates/${taskId}`],
  ]) {
    await page.goto(oldPath!)
    await expect.poll(() => new URL(page.url()).pathname).toBe(expected)
    await expect(page.getByRole('heading').first()).toBeVisible()
    expect(new URL(page.url()).hash).toBe('')
  }
  expect(queries).toBe(0); expect(writes).toBe(0)
})

test('F03 logout removes cached backend results and stops page polling', async ({ page }) => {
  await page.clock.install()
  await setup(page)
  const fixture = backendCapture('students.query')
  let queryCount = 0, protectedReads = 0
  page.on('request', request => { if (request.url().includes('/admin/') || /\/console-api\/(query|worker)\//.test(request.url())) protectedReads++ })
  await page.route('**/console-api/query/api/v1/sicp/students/query', route => { queryCount++; return route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }) })
  await openQueries(page)
  await page.getByRole('button', { name: '学生信息', exact: true }).click()
  await page.getByRole('button', { name: '查询数据', exact: true }).click()
  await expect(page.getByRole('table')).toContainText('虚构学生001')
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/login$/)
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  expect(await page.locator('body').textContent()).not.toContain('虚构学生001')
  const stopped = protectedReads
  await page.clock.runFor(65000)
  expect(protectedReads).toBe(stopped)
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  await page.getByLabel('管理员账号').fill('admin')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await openQueries(page)
  await expect(page.getByRole('heading', { name: '等待查询', exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toHaveCount(0)
  expect(queryCount).toBe(1)
})

test('F04 overview retains timestamped backend observations on failure without inventing log readiness', async ({ page }, testInfo) => {
  await setup(page)
  const fixture = managementCapture('overview')
  const observedAt = (fixture.response.data as { observed_at: string }).observed_at
  const snapshot = page.getByRole('region', { name: '运行快照', exact: true })
  const logs = page.getByRole('region', { name: '日志状态', exact: true })
  await expect(snapshot.locator('time')).toHaveAttribute('datetime', observedAt)
  await expect(logs).toContainText('平台日志')
  await expect(logs).toContainText('上游日志')
  await expect(logs).toContainText('未观测')
  await expect(logs.locator('.success')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  const query = page.getByRole('article').filter({ hasText: '业务查询服务' })
  await page.route('**/console-api/query/readyz', route => route.fulfill({ json: { ready: true } }))
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(query.locator('.tag')).toHaveText('✓ 已就绪')
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  await page.route('**/admin/v1/overview', route => route.fulfill({ status: 503, json: { code: 'unavailable', data: null } }))
  await page.route('**/admin/v1/system/status', route => route.abort())
  await page.route('**/console-api/query/readyz', route => route.abort())
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(snapshot).toContainText('显示上次成功读取')
  await expect(snapshot.locator('time')).toHaveAttribute('datetime', observedAt)
  await expect(logs).toContainText('显示上次成功读取')
  await expect(query.locator('.tag')).toHaveText('当前状态未确认')
  await expect(query.locator('.success')).toHaveCount(0)
  await expect(query).toContainText('就绪状态与依赖沿用上次快照')
  await query.locator('summary').filter({ hasText: '服务详情' }).click()
  await expect(query.getByText('可用', { exact: true })).toBeVisible()
  await expect(query.locator('time')).toHaveCount(1)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('overview-stale-mobile.png'), fullPage: true })
})

test('F04 compact overview uses recorded source rows and separates missing event times', async ({ page }, testInfo) => {
  await setup(page)
  const source = (managementCapture('sources').response.data as { items: { id: string }[] }).items[0]!
  const summary = page.locator('.overview-summary')
  await expect(summary).toContainText('内容发布')
  await expect(summary).toContainText('成功检查')
  await expect(summary.locator('time')).toHaveCount(3)
  await expect(summary.locator('time[datetime]')).toHaveCount(0)
  const table = page.getByRole('region', { name: '数据源状态表', exact: true })
  await expect(table.getByRole('row')).toHaveCount(2)
  await expect(table).toContainText(source.id)
  await expect(table).toContainText('无失败记录')
  await expect(table.locator('.success')).toHaveCount(0)
  await expect(page.getByRole('region', { name: '最近更新', exact: true })).toContainText('最近更新记录为空')
  const service = page.getByRole('article').filter({ hasText: '业务查询服务' })
  await expect(service.getByText('进程存活', { exact: true })).not.toBeVisible()
  await service.locator('summary').filter({ hasText: '服务详情' }).click()
  await expect(service.getByText('进程存活', { exact: true })).toBeVisible()
  await service.locator('summary').filter({ hasText: '服务详情' }).click()
  await page.screenshot({ path: testInfo.outputPath('compact-overview-desktop.png'), fullPage: true })
  for (const width of [1280, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  await page.screenshot({ path: testInfo.outputPath('compact-overview-mobile.png'), fullPage: true })
  await page.route('**/admin/v1/sources', route => route.abort())
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  const sources = page.getByRole('region', { name: '数据源状态', exact: true })
  await expect(sources).toContainText('沿用旧快照，当前状态未确认')
  await expect(table).toContainText(source.id)
})

test('F04 sources filter only recorded rows and keep stale details with keyboard return', async ({ page }, testInfo) => {
  await setup(page)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  const fixture = managementCapture('sources')
  const source = (fixture.response.data as { items: { id: string; primary_key: string }[] }).items[0]!
  const calls: string[] = []
  let fail = false
  await page.route('**/admin/v1/sources', route => {
    calls.push(route.request().url())
    return fail ? route.abort() : route.fulfill({ json: fixture.response })
  })
  await page.getByRole('link', { name: '数据源', exact: true }).click()
  const table = page.getByRole('region', { name: '数据源表格，可横向滚动' })
  await expect(table.getByRole('row')).toHaveCount(2)
  await expect(table.getByRole('cell', { name: source.primary_key, exact: true })).toBeVisible()
  const count = calls.length
  await page.getByLabel('源名称或标识').fill(source.id.toUpperCase())
  await page.getByLabel('已获取的失败状态').selectOption('none')
  await expect(table.getByRole('row')).toHaveCount(2)
  await page.getByLabel('源名称或标识').fill('no matching source')
  await expect(page.getByRole('heading', { name: '没有符合本地筛选的源' })).toBeVisible()
  await page.getByRole('button', { name: '清除筛选', exact: true }).click()
  const opener = page.getByRole('button', { name: `查看 ${source.id} 详情` })
  await opener.click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('后端未提供当前窗口')
  await expect(drawer).toContainText('后端未提供失败记录')
  await expect(drawer.locator('.success')).toHaveCount(0)
  expect(calls.length).toBe(count)
  expect(calls.every(url => new URL(url).search === '')).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('sources-drawer-desktop.png'), fullPage: true })
  await page.keyboard.press('Escape')
  await expect(opener).toBeFocused()
  fail = true
  await page.getByRole('button', { name: '刷新源状态' }).click()
  await expect(page.getByText('以下为上次成功读取的快照', { exact: false })).toBeVisible()
  await opener.click()
  await expect(drawer).toContainText('旧快照，当前状态未确认')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('sources-drawer-mobile.png'), fullPage: true })
})

test('F04 source polling stops off-page and logout removes its drawer', async ({ page }) => {
  await page.clock.install()
  await setup(page)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  const fixture = managementCapture('sources')
  let calls = 0
  await page.route('**/admin/v1/sources', route => { calls++; return route.fulfill({ json: fixture.response }) })
  await page.getByRole('link', { name: '数据源', exact: true }).click()
  await expect(page.getByRole('button', { name: '刷新源状态' })).toBeEnabled()
  const first = calls
  await page.clock.runFor(15100)
  await expect.poll(() => calls).toBeGreaterThan(first)
  await expect(page.getByRole('button', { name: '刷新源状态' })).toBeEnabled()
  await page.getByRole('link', { name: '通知中心', exact: true }).click()
  await expect(page.getByRole('heading', { name: '通知中心', exact: true })).toBeVisible()
  const paused = calls
  await page.clock.runFor(61000)
  expect(calls).toBe(paused)
  await page.getByRole('link', { name: '数据源', exact: true }).click()
  await expect.poll(() => calls).toBeGreaterThan(paused)
  await page.getByRole('button', { name: /查看 .* 详情/ }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const exited = calls
  await page.clock.runFor(61000)
  expect(calls).toBe(exited)
})

test('F04 empty initialization directs to existing progress without exposing manual bootstrap', async ({ page }) => {
  await setup(page)
  const fixture = managementCapture('overview')
  const tasks = (fixture.response.data as { tasks: unknown }).tasks
  let posts = 0
  await page.route('**/admin/v1/tasks/**', route => {
    if (route.request().method() === 'POST') posts++
    return route.fulfill({ json: { code: 'ok', data: tasks } })
  })
  const notice = page.getByRole('region', { name: '初始化状态', exact: true })
  await expect(notice).toContainText('等待自动初始化')
  await expect(notice).toContainText('首次初始化完成前不能进行业务查询')
  await expect(notice.getByRole('button')).toHaveCount(0)
  await notice.getByRole('link', { name: '查看初始化进度 →' }).click()
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '强制全量更新', exact: true })).toBeDisabled()
  await page.getByRole('radio', { name: /单表强制更新/ }).check()
  await expect(page.getByRole('button', { name: '提交更新', exact: true })).toBeDisabled()
  await expect(page.getByText('当前不可执行单表更新', { exact: false })).toBeVisible()
  expect(posts).toBe(0)
  await page.getByRole('link', { name: '运行总览', exact: true }).click()
  await page.route('**/admin/v1/overview', route => route.abort())
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect(notice).toContainText('初始化状态未确认')
  await expect(notice.getByRole('link')).toHaveCount(0)
})

test('F05 catalog searches the 23 backend entries without executing or inventing interfaces', async ({ page }, testInfo) => {
  await setup(page)
  let executions = 0, reads = 0
  const items = backendCatalog() as { id: string; kind: string; version: string; path: string }[]
  await page.route('**/admin/v1/apis', route => { reads++; return route.fulfill({ json: { code: 'ok', data: { version: items[0]!.version, items } } }) })
  await page.route('**/console-api/query/api/**', route => { executions++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => { executions++; return route.abort() })
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  const table = page.getByRole('region', { name: '接口目录表格' })
  await expect(table.getByRole('row')).toHaveCount(10)
  await expect(page.getByText(/后端目录 23 项/)).toBeVisible()
  await expect(table.locator('.success')).toHaveCount(0)
  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(table.getByRole('row')).toHaveCount(24)
  await page.getByLabel('名称、路径或分组').fill('上游接口')
  await expect(table.getByRole('row')).toHaveCount(15)
  await page.getByLabel('名称、路径或分组').fill('/students/grades/query')
  await expect(table.getByRole('row')).toHaveCount(2)
  await page.getByLabel('名称、路径或分组').fill('学生成绩')
  await expect(table.getByRole('row')).toHaveCount(2)
  await page.getByLabel('名称、路径或分组').fill('no matching API')
  await expect(page.getByRole('heading', { name: '没有符合条件的接口' })).toBeVisible()
  await page.getByRole('button', { name: '清除筛选' }).click()
  await page.getByRole('combobox', { name: '接口分组', exact: true }).selectOption('business')
  const notes = page.getByRole('region', { name: '补充说明' })
  await expect(notes).toContainText('行政班查询 · 契约待定')
  await expect(notes.getByRole('button')).toHaveCount(0)
  expect(executions).toBe(0)
  expect(reads).toBe(1)
  await page.screenshot({ path: testInfo.outputPath('api-catalog-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('api-catalog-mobile.png'), fullPage: true })
  await page.route('**/admin/v1/apis', route => route.abort())
  await page.getByRole('button', { name: '刷新目录', exact: true }).click()
  await expect(page.getByText('以下为上次成功读取的目录', { exact: false })).toBeVisible()
  await expect(table.getByRole('row')).toHaveCount(10)
})

test('F05 details keep endpoint and readiness visible while tabs stay read-only', async ({ page }, testInfo) => {
  await setup(page)
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toBeEnabled()
  const items = backendCatalog() as { id: string; method: string; path: string; kind: string }[]
  const api = items.find(item => item.id === 'students.grades.query')!
  let reads = 0, health = 0, executions = 0
  await page.route(`**/admin/v1/apis/${api.id}`, route => { reads++; return route.fulfill({ json: { code: 'ok', data: api } }) })
  await page.route('**/console-api/query/readyz', route => { health++; return route.fulfill({ json: { ready: true } }) })
  await page.route('**/console-api/query/api/**', route => { executions++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => { executions++; return route.abort() })
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  await page.getByRole('link', { name: '查看 学生成绩 文档', exact: true }).click()
  await expect(page.getByRole('heading', { name: '学生成绩', exact: true })).toBeVisible()
  const info = page.getByRole('region', { name: '接口信息', exact: true })
  await expect(info).toContainText(api.path)
  await expect(info).toContainText('查询服务已就绪')
  const buttons = page.getByRole('tab')
  await expect(buttons).toHaveCount(5)
  for (const label of ['参数', '请求案例', '响应案例', '调用记录', '概览']) {
    await page.getByRole('tab', { name: label, exact: true }).click()
    await expect(page.getByRole('tabpanel')).toBeVisible()
    await expect(page.getByRole('tab', { name: label, exact: true })).toHaveAttribute('aria-controls', 'api-doc-panel')
    await expect(info).toContainText(api.path)
  }
  await page.getByRole('tab', { name: '概览' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: '参数' })).toBeFocused()
  await expect(page.getByRole('tab', { name: '参数' })).toHaveAttribute('aria-selected', 'true')
  expect(reads).toBe(1)
  expect(health).toBe(1)
  expect(executions).toBe(0)
  await page.screenshot({ path: testInfo.outputPath('api-detail-desktop.png'), fullPage: true })
  await page.route('**/console-api/query/readyz', route => route.abort())
  await page.getByRole('button', { name: '刷新详情', exact: true }).click()
  await expect(info).toContainText('查询服务状态未确认')
  await expect(info.locator('.success')).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('api-detail-mobile.png'), fullPage: true })
})

test('F05 source detail stays unprobed and unknown API is not fabricated', async ({ page }) => {
  await setup(page)
  const api = (backendCatalog() as { id: string; kind: string; path: string }[]).find(item => item.kind === 'source')!
  await page.goto(`/console/apis/${api.id}`)
  const info = page.getByRole('region', { name: '接口信息', exact: true })
  await expect(info).toContainText(api.path)
  await expect(info).toContainText('上游就绪未探测')
  await expect(info.locator('.success')).toHaveCount(0)
  await page.getByRole('tab', { name: '调用记录' }).click()
  await expect(page.getByRole('tabpanel')).toContainText('尚未加载调用记录')
  await page.goto('/console/apis/not-a-real-api')
  await expect(page.getByRole('alert')).toContainText('未找到请求的对象')
  await expect(page.getByRole('region', { name: '接口信息', exact: true })).toHaveCount(0)
})

test('F05 recursive fields preserve backend defaults and nullable nested responses on mobile', async ({ page }, testInfo) => {
  await setup(page)
  await page.goto('/console/apis/students.query')
  await page.getByRole('tab', { name: '参数', exact: true }).click()
  const request = page.getByRole('region', { name: '参数字段表', exact: true })
  await expect(request.getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^page_size$/ }) })).toContainText('1000')
  await page.getByRole('tab', { name: '响应案例', exact: true }).click()
  const response = page.getByRole('region', { name: '响应字段表', exact: true })
  const nested = response.getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^items\[\]\.department\.is_active$/ }) })
  await expect(nested).toContainText('是否有效')
  await expect(nested.getByRole('cell').nth(1)).toHaveText('是')
  await expect(nested.getByRole('cell').nth(2)).toHaveText('是')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await response.focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => response.evaluate(el => el.scrollLeft)).toBeGreaterThan(0)
  await page.screenshot({ path: testInfo.outputPath('schema-mobile.png'), fullPage: true })
  const source = (backendCatalog() as { id: string; kind: string }[]).find(api => api.id === 'dwd_yxsdwjbsjzlb')!
  await page.goto(`/console/apis/${source.id}`)
  await page.getByRole('tab', { name: '响应案例', exact: true }).click()
  await expect(page.getByRole('region', { name: '响应字段表' }).getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^DWH$/ }) })).toContainText('单位号')
})

test('F05 provider Query and controlled test Body have separate fields and defaults', async ({ page }, testInfo) => {
  await setup(page)
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('tab', { name: '参数', exact: true }).click()
  const query = page.getByRole('region', { name: '上游 GET Query', exact: true })
  const job = page.getByRole('region', { name: '浏览器受控测试 JSON', exact: true })
  await expect(query).toContainText('是示例')
  await expect(query.getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^pageSize$/ }) })).toContainText('未提供')
  await expect(query.getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^XH$/ }) })).toContainText('学号')
  await expect(job).toContainText('page_size → pageSize')
  await expect(job).toContainText('POST /admin/v1/api-tests')
  await expect(job).toContainText('不接受自定义地址、令牌')
  await expect(job.getByRole('row').filter({ has: page.locator('th code').filter({ hasText: /^page_size$/ }) })).toContainText('1..100')
  await expect(page.getByRole('tabpanel').getByRole('textbox')).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('source-params-mobile.png'), fullPage: true })
  await page.getByRole('tab', { name: '请求案例', exact: true }).click()
  await expect(page.getByRole('heading', { name: '受控测试作业 JSON 案例' })).toBeVisible()
})

test('F05 loading examples only fills the current API draft and uses credential placeholders', async ({ page }) => {
  await setup(page)
  let executions = 0
  await page.route('**/console-api/query/api/**', route => { executions++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => { executions++; return route.abort() })
  await page.goto('/console/apis/students.grades.query')
  await page.getByRole('tab', { name: '请求案例', exact: true }).click()
  const sample = (backendCatalog() as { id: string; request_example: unknown }[]).find(api => api.id === 'students.grades.query')!
  await page.getByRole('button', { name: '使用此案例测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('学生成绩 · 在线测试')
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toHaveValue(JSON.stringify(sample.request_example, null, 2))
  await expect(dialog.getByRole('button', { name: '执行', exact: true })).toHaveCount(0)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '使用此案例测试', exact: true })).toBeFocused()
  await page.getByRole('combobox', { name: '案例类型', exact: true }).selectOption('next')
  await page.getByText('复制 curl 命令（POSIX shell）', { exact: true }).click()
  await expect(page.getByLabel('curl 命令', { exact: true })).toContainText('X-API-Key: <QUERY_API_KEY>')
  await expect(page.getByLabel('curl 命令', { exact: true })).toContainText('X-Query-Snapshot: <SAME_QUERY_SNAPSHOT>')
  await page.getByRole('button', { name: '使用此案例测试', exact: true }).click()
  await expect(dialog).toContainText('未携带真实快照')
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toHaveValue(/"page": 2/)
  await page.keyboard.press('Escape')
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('tab', { name: '请求案例', exact: true }).click()
  await page.getByRole('button', { name: '使用此案例测试', exact: true }).click()
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toHaveValue(/"page_size": 10/)
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).not.toHaveValue(/student_number/)
  expect(executions).toBe(0)
})

test('F05 call records use operation and cursor contracts and retain failed-read context', async ({ page }) => {
  await setup(page)
  const history = managementCapture('log_history').response.data as Record<string, any>
  const queries: URLSearchParams[] = []
  const opaqueCursor = (managementCapture('log_live').response.data as { stream_cursor: string }).stream_cursor // Opaque forwarding boundary; no invented event rows.
  await page.route('**/admin/v1/logs/platform/events?*', route => {
    const params = new URL(route.request().url()).searchParams; queries.push(params)
    return route.fulfill({ json: { code: 'ok', data: { ...history, source: 'platform', filter: { ...history.filter, operation: 'students.query' }, next_cursor: queries.length === 1 ? opaqueCursor : null } } })
  })
  await page.goto('/console/apis/students.query')
  await page.getByRole('tab', { name: '响应案例', exact: true }).click()
  await expect(page.getByRole('region', { name: '完整 HTTP 响应契约' })).toContainText('code 是字符串')
  expect(queries).toHaveLength(0)
  await page.getByRole('tab', { name: '调用记录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '此时间范围内没有匹配的日志事件' })).toBeVisible()
  expect(queries).toHaveLength(1)
  expect(queries[0]!.get('operation')).toBe('students.query')
  expect(queries[0]!.get('mode')).toBe('history')
  await page.getByRole('button', { name: '读取下一批', exact: true }).click()
  await expect.poll(() => queries.length).toBe(2)
  expect([...queries[1]!.keys()]).toEqual(['cursor'])
  expect(queries[1]!.get('cursor')).toBe(opaqueCursor)
  await expect(page.getByText('已到本次日志范围末批。')).toBeVisible()
  await page.route('**/admin/v1/logs/platform/events?*', route => route.fulfill({ status: 503, json: { code: 'unavailable', data: null } }))
  await page.getByRole('button', { name: '重新读取首批', exact: true }).click()
  await expect(page.getByText('当前读取失败，以下保留上次成功读取的批次', { exact: false })).toBeVisible()
  await expect(page.getByRole('alert')).toBeVisible()
})

test('F05 source calls require a real job identifier and never invent a source-operation filter', async ({ page }) => {
  await setup(page)
  const job = managementCapture('source_test').response.data as { id: string }
  let reads = 0
  await page.route('**/admin/v1/logs/upstream/events?*', route => {
    reads++
    const params = new URL(route.request().url()).searchParams
    expect(params.get('test_run_id')).toBe(job.id)
    expect(params.get('id_format')).toBe('raw')
    expect(params.has('operation')).toBe(false)
    expect(params.has('api_id')).toBe(false)
    return route.fulfill({ status: 503, json: { code: 'unavailable', data: null } })
  })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('tab', { name: '响应案例', exact: true }).click()
  await expect(page.getByRole('region', { name: '完整 HTTP 响应契约' })).toContainText('result 是受控采集结果的 total/list')
  await page.getByRole('tab', { name: '调用记录', exact: true }).click()
  await expect(page.getByRole('button', { name: '查询关联记录' })).toBeDisabled()
  expect(reads).toBe(0)
  await page.getByLabel('测试作业 ID', { exact: true }).fill(job.id)
  await page.getByRole('button', { name: '查询关联记录' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  expect(reads).toBe(1)
  expect(page.url()).not.toContain(job.id)
  await page.getByRole('tab', { name: '概览', exact: true }).click()
  await page.getByRole('tab', { name: '调用记录', exact: true }).click()
  await expect(page.getByLabel('测试作业 ID', { exact: true })).toHaveValue('')
})

test('F05 management documentation only navigates to dedicated workflows and performs no writes', async ({ page }, testInfo) => {
  await setup(page)
  let writes = 0
  page.on('request', request => { if (['POST', 'PUT', 'DELETE'].includes(request.method())) writes++ })
  await page.getByRole('link', { name: '接口中心', exact: true }).click()
  const docs = page.getByRole('region', { name: '管理写接口说明', exact: true })
  await expect(docs).toContainText('静态说明基线：2026-10-14')
  await expect(docs).toContainText('页面不读取或回显')
  await docs.getByText('更新与修复', { exact: true }).click()
  await expect(docs.getByText('POST /admin/v1/tasks/incremental', { exact: true })).toBeVisible()
  await expect(docs).toContainText('Idempotency-Key')
  await docs.getByText('通知设置与测试', { exact: true }).click()
  await expect(docs.getByText('PUT /admin/v1/settings/notifications', { exact: true })).toBeVisible()
  await docs.getByText('日志保留与清理', { exact: true }).click()
  await expect(docs.getByText('POST /admin/v1/maintenance/cleanup', { exact: true })).toBeVisible()
  await expect(docs.getByRole('textbox')).toHaveCount(0)
  await expect(docs.getByRole('link', { name: '前往系统设置 →', exact: true })).toHaveAttribute('href', '/console/settings')
  await expect(docs.getByRole('link', { name: '前往通知中心 →', exact: true })).toHaveAttribute('href', '/console/notifications')
  await expect(page.getByText(/后端目录 23 项/)).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await docs.screenshot({ path: testInfo.outputPath('management-docs-mobile.png') })
  await docs.getByRole('link', { name: '前往更新事件 →', exact: true }).click()
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
  expect(writes).toBe(0)
})

test('F06 test dialog binds to the selected backend API with fresh drafts and no execution', async ({ page }, testInfo) => {
  await setup(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/console/apis/students.query')
  await expect(page.getByRole('button', { name: '在线测试', exact: true })).toBeEnabled()
  let executions = 0
  await page.route('**/console-api/query/api/**', route => { executions++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => { executions++; return route.abort() })
  const api = (backendCatalog() as { id: string; version: string; path: string }[]).find(api => api.id === 'students.query')!
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText(api.version)
  await expect(dialog).toContainText(api.path)
  await expect(dialog.getByRole('heading', { name: '尚未测试', exact: true })).toBeVisible()
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toHaveValue('{}')
  const box = await dialog.boundingBox()
  expect(box!.width).toBe(960)
  expect(box!.height).toBeLessThanOrEqual(900)
  await dialog.getByRole('button', { name: '展开窗口', exact: true }).click()
  expect((await dialog.boundingBox())!.width).toBe(1392)
  await dialog.getByLabel('请求 JSON', { exact: true }).fill('{ invalid draft')
  await page.screenshot({ path: testInfo.outputPath('test-dialog-desktop.png') })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '在线测试', exact: true })).toBeFocused()
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toHaveValue('{}')
  await page.setViewportSize({ width: 390, height: 844 })
  const mobile = await dialog.boundingBox()
  expect(mobile).toMatchObject({ x: 0, y: 0, width: 390, height: 844 })
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('test-dialog-mobile.png') })
  await dialog.getByRole('button', { name: '关闭测试', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await page.goto('/console/apis/staff.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  await expect(dialog).toContainText('staff.query')
  await expect(dialog).not.toContainText('students.query')
  expect(executions).toBe(0)
})

test('F06 a changed catalog version closes the old test instance without sending', async ({ page }) => {
  await setup(page)
  await page.goto('/console/apis/students.query')
  await page.getByRole('tab', { name: '请求案例', exact: true }).click()
  const api = (backendCatalog() as { id: string; version: string }[]).find(api => api.id === 'students.query')!
  let release: (() => void) | undefined
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route('**/admin/v1/apis/students.query', async route => {
    await pending
    // Catalog-version protocol boundary; every field and example retains the backend fixture.
    await route.fulfill({ json: { code: 'ok', data: { ...api, version: api.version + '-next' } } })
  })
  await page.getByRole('button', { name: '刷新详情', exact: true }).click()
  await page.getByRole('button', { name: '使用此案例测试', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  release!()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('region', { name: '接口信息', exact: true })).toContainText(api.version + '-next')
})

test('F06 editor validates real backend requests, preserves invalid drafts and switches common and advanced fields', async ({ page }, testInfo) => {
  await setup(page)
  await page.goto('/console/apis/students.grades.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog'), json = dialog.getByLabel('请求 JSON', { exact: true })
  let executions = 0
  await page.route('**/console-api/query/api/**', route => { executions++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => { executions++; return route.abort() })
  await dialog.getByRole('button', { name: '校验参数', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('student_number')
  await json.fill('{"student_number":')
  await dialog.getByRole('button', { name: '参数编辑', exact: true }).click()
  await expect(json).toHaveValue('{"student_number":')
  const captured = backendCapture('students.grades.query')
  const original = JSON.stringify(captured.request, null, 4) + '\n'
  await json.fill(original)
  await dialog.getByRole('button', { name: '校验参数', exact: true }).click()
  await expect(dialog.getByText('参数校验通过；尚未发送请求。', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '参数编辑', exact: true }).click()
  await expect(dialog.getByRole('textbox', { name: 'student_number', exact: true })).toHaveValue(JSON.stringify(captured.request.student_number))
  await expect(dialog.getByRole('textbox', { name: 'score_min', exact: true })).toBeHidden()
  await dialog.getByRole('button', { name: /显示高级参数/ }).click()
  await expect(dialog.getByRole('textbox', { name: 'score_min', exact: true })).toBeVisible()
  await dialog.getByRole('textbox', { name: 'score_min', exact: true }).fill('2')
  await dialog.getByRole('textbox', { name: 'score_max', exact: true }).fill('1')
  await dialog.getByRole('button', { name: '校验参数', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('不能大于')
  await expect(dialog.getByRole('textbox', { name: 'score_min', exact: true })).toHaveValue('2')
  await dialog.getByRole('textbox', { name: 'score_max', exact: true }).fill('2')
  await dialog.getByRole('button', { name: '校验参数', exact: true }).click()
  await expect(dialog.getByText('参数校验通过；尚未发送请求。', { exact: true })).toBeVisible()
  expect(JSON.parse(await json.inputValue()).student_number).toBe(captured.request.student_number)
  await dialog.getByRole('button', { name: '重置参数', exact: true }).click()
  await expect(json).toHaveValue('{}')
  await dialog.getByRole('combobox', { name: '填入案例', exact: true }).selectOption('invalid')
  await dialog.getByRole('button', { name: '填入所选案例', exact: true }).click()
  await dialog.getByRole('button', { name: '校验参数', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('page')
  await expect(json).toHaveValue(/"page": 0/)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('test-parameters-mobile.png') })
  expect(executions).toBe(0)
})

for (const operation of operations) test(`F06 business execution ${operation.operation} replays the existing backend capture once`, async ({ page }, testInfo) => {
  await setup(page)
  const fixture = backendCapture(operation.operation), raw = JSON.stringify(fixture.response, null, 2) + '\n'
  let queries = 0, jobs = 0
  await page.route(`**/console-api/query${operation.path}`, route => {
    queries++
    expect(route.request().method()).toBe('POST')
    expect(route.request().postDataJSON()).toEqual(fixture.request)
    expect(route.request().headers()['x-query-snapshot']).toBeUndefined()
    return route.fulfill({ status: fixture.status, headers: fixture.headers, body: raw })
  })
  await page.route('**/admin/v1/api-tests', route => { jobs++; return route.abort() })
  await page.goto(`/console/apis/${operation.operation}`)
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(fixture.request))
  expect(queries).toBe(0)
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect(dialog.getByRole('region', { name: '响应内容', exact: true })).toContainText(`HTTP ${fixture.status}`)
  await expect(dialog.getByLabel('响应原文', { exact: true })).toHaveText(raw.slice(0, 4096))
  await expect(dialog.getByRole('region', { name: '响应内容', exact: true })).toContainText(fixture.headers['X-Request-ID'] || fixture.response.request_id!)
  await dialog.getByText('实际响应头', { exact: true }).click()
  await expect(dialog.getByLabel('响应头', { exact: true })).toContainText('X-SICP-Combination')
  expect(queries).toBe(1)
  expect(jobs).toBe(0)
  if (operation.operation === 'students.query') {
    await page.setViewportSize({ width: 390, height: 844 })
    await dialog.getByRole('region', { name: '响应内容', exact: true }).scrollIntoViewIfNeeded()
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('business-response-mobile.png') })
  }
  await dialog.getByLabel('请求 JSON', { exact: true }).fill('{}')
  await expect(dialog.getByText('上次读取结果，当前请求尚未取得新响应。', { exact: true })).toBeVisible()
})

test('F06 double execution is guarded and closed instances ignore late actual responses', async ({ page }) => {
  await setup(page)
  const fixture = backendCapture('students.query')
  let calls = 0, release: (() => void) | undefined, complete: (() => void) | undefined
  const pending = new Promise<void>(resolve => { release = resolve }), completed = new Promise<void>(resolve => { complete = resolve })
  await page.route('**/console-api/query/api/v1/sicp/students/query', async route => {
    calls++
    if (calls === 1) await pending
    await route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }).catch(() => {})
    complete!()
  })
  await page.goto('/console/apis/students.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行测试', exact: true }).evaluate(element => { (element as HTMLButtonElement).click(); (element as HTMLButtonElement).click() })
  await expect.poll(() => calls).toBe(1)
  await expect(dialog.getByRole('button', { name: '等待响应…', exact: true })).toBeDisabled()
  await dialog.getByRole('button', { name: '停止等待并关闭', exact: true }).click()
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  release!(); await completed
  await expect(dialog.getByRole('heading', { name: '尚未测试', exact: true })).toBeVisible()
  await expect(dialog.getByRole('region', { name: '响应内容', exact: true })).toHaveCount(0)
  expect(calls).toBe(1)
})

test('F06 source executes one fixed page then reads Location and displays unchanged mock result text', async ({ page }, testInfo) => {
  await setup(page)
  const api = (backendCatalog() as { id: string; version: string }[]).find(api => api.id === 'dwd_bzkscjzlb')!
  const accepted = managementCapture('source_test'), queued = managementCapture('test_result'), done = sourceJobResult(api.id)
  let posts = 0, gets = 0, queries = 0, submitted: Record<string, unknown> | undefined
  await page.route('**/console-api/query/api/**', route => { queries++; return route.abort() })
  await page.route('**/admin/v1/api-tests', route => {
    posts++; submitted = route.request().postDataJSON()
    expect(submitted).toEqual({ api_id: api.id, version: api.version, page: 1, page_size: 10, idempotency_key: expect.any(String) })
    expect(route.request().headers()['idempotency-key']).toBeUndefined()
    return route.fulfill({ status: accepted.status, headers: { Location: queued.path }, json: accepted.response })
  })
  await page.route(`**${queued.path}`, route => {
    gets++; expect(route.request().method()).toBe('GET')
    return gets === 1 ? route.fulfill({ status: queued.status, json: queued.response }) : route.fulfill({ status: done.status, body: done.raw })
  })
  await page.goto(`/console/apis/${api.id}`)
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  expect(posts).toBe(0); expect(gets).toBe(0)
  await dialog.getByLabel('请求 JSON', { exact: true }).fill('{"page":null,"page_size":0}')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).evaluate(element => { (element as HTMLButtonElement).click(); (element as HTMLButtonElement).click() })
  await expect(dialog).toContainText('已受理，排队中')
  await expect(dialog).toContainText('运行中尚无结果不代表失败')
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toBeDisabled()
  await expect(dialog).toContainText('作业执行成功')
  await expect(dialog.getByLabel('本页源数据 JSON', { exact: true })).toHaveText(done.resultText.slice(0, 4096))
  await expect(dialog.getByLabel('管理作业响应原文', { exact: true })).toHaveText(done.raw.slice(0, 4096))
  expect(posts).toBe(1); expect(gets).toBe(2); expect(queries).toBe(0)
  expect(await page.evaluate(() => sessionStorage.getItem('sicp.pending-operations.v1'))).toBeNull()
  await page.setViewportSize({ width: 390, height: 844 })
  await dialog.getByRole('heading', { name: '本页源数据', exact: true }).scrollIntoViewIfNeeded()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('source-test-mobile.png') })
  await dialog.getByRole('button', { name: '结束本次，准备新测试', exact: true }).click()
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toBeEnabled()
  await expect(dialog.getByRole('heading', { name: '尚未测试', exact: true })).toBeVisible()
  expect(posts).toBe(1)
})

test('F06 source lost admission reply reopens without sending and retries the original key and payload', async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), done = sourceJobResult('dwd_bzkscjzlb')
  const payloads: string[] = []
  await page.route('**/admin/v1/api-tests', route => {
    payloads.push(route.request().postData()!)
    return payloads.length === 1 ? route.abort() : route.fulfill({ status: accepted.status, headers: { Location: done.path }, json: accepted.response })
  })
  await page.route(`**${done.path}`, route => route.fulfill({ status: done.status, body: done.raw }))
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(dialog).toContainText('受理结果待确认')
  await expect(dialog.getByRole('alert')).toContainText('受理结果未知')
  await dialog.getByRole('button', { name: '关闭测试', exact: true }).click()
  await page.reload()
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  await expect(dialog.getByRole('button', { name: '原键重试受理', exact: true })).toBeEnabled()
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toBeDisabled()
  expect(payloads).toHaveLength(1)
  await dialog.getByRole('button', { name: '原键重试受理', exact: true }).click()
  await expect(dialog).toContainText('作业执行成功')
  expect(payloads).toHaveLength(2)
  expect(payloads[0]).toBe(payloads[1])
})

test('F06 source accepted job closes polling and reopens with explicit GET only', async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), queued = managementCapture('test_result'), done = sourceJobResult('dwd_bzkscjzlb')
  let posts = 0, gets = 0, finish = false
  await page.route('**/admin/v1/api-tests', route => { posts++; return route.fulfill({ status: accepted.status, headers: { Location: queued.path }, json: accepted.response }) })
  await page.route(`**${queued.path}`, route => { gets++; return finish ? route.fulfill({ status: done.status, body: done.raw }) : route.fulfill({ status: queued.status, json: queued.response }) })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(dialog).toContainText('已受理，排队中')
  await expect.poll(() => gets).toBeGreaterThan(0)
  await dialog.getByRole('button', { name: '停止等待并关闭', exact: true }).click()
  const stoppedAt = gets
  const saved = await page.evaluate(() => sessionStorage.getItem('sicp.pending-operations.v1'))
  expect(saved).toContain(queued.path); expect(saved).not.toContain('result_state')
  finish = true
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  await expect(dialog.getByRole('button', { name: '查询原作业', exact: true })).toBeEnabled()
  expect(gets).toBe(stoppedAt); expect(posts).toBe(1)
  await dialog.getByRole('button', { name: '查询原作业', exact: true }).click()
  await expect(dialog).toContainText('作业执行成功')
  expect(gets).toBe(stoppedAt + 1); expect(posts).toBe(1)
})

test('F06 source read failure stops the chain and explicit recovery never POSTs again', async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), done = sourceJobResult('dwd_bzkscjzlb')
  let posts = 0, gets = 0
  await page.route('**/admin/v1/api-tests', route => { posts++; return route.fulfill({ status: accepted.status, headers: { Location: done.path }, json: accepted.response }) })
  await page.route(`**${done.path}`, route => { gets++; return gets === 1 ? route.fulfill({ status: 502, contentType: 'text/html', body: '<h1>Bad Gateway</h1>' }) : route.fulfill({ status: done.status, body: done.raw }) })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('HTTP 502')
  await expect(dialog.getByRole('button', { name: '查询原作业', exact: true })).toBeEnabled()
  expect(posts).toBe(1); expect(gets).toBe(1)
  await dialog.getByRole('button', { name: '查询原作业', exact: true }).click()
  await expect(dialog).toContainText('作业执行成功')
  expect(posts).toBe(1); expect(gets).toBe(2)
})

test('F06 source polling is bounded after a refused request becomes accepted and keeps its pending key', async ({ page }) => {
  await setup(page)
  await page.clock.install()
  const accepted = managementCapture('source_test'), queued = managementCapture('test_result'), refused = backendCapture('students.query', 'invalid_argument')
  let posts = 0, gets = 0
  const payloads: string[] = []
  await page.route('**/admin/v1/api-tests', route => {
    posts++; payloads.push(route.request().postData()!)
    return posts === 1 ? route.fulfill({ status: refused.status, json: refused.response }) : route.fulfill({ status: accepted.status, headers: { Location: queued.path }, json: accepted.response })
  })
  await page.route(`**${queued.path}`, route => { gets++; return route.fulfill({ status: queued.status, json: queued.response }) })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(dialog).toContainText('服务端拒绝受理')
  await expect(dialog.getByRole('button', { name: '结束本次，准备新测试', exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '原键重试受理', exact: true }).click()
  await expect.poll(() => gets).toBe(1)
  for (let count = 2; count <= 15; count++) { await page.clock.runFor(2001); await expect.poll(() => gets).toBe(count) }
  await expect(dialog).toContainText('本轮等待已达上限')
  await expect(dialog.getByRole('button', { name: '结束本次，准备新测试', exact: true })).toHaveCount(0)
  await expect(dialog.getByLabel('请求 JSON', { exact: true })).toBeDisabled()
  await page.clock.runFor(10000)
  expect(gets).toBe(15); expect(posts).toBe(2); expect(payloads[0]).toBe(payloads[1])
})

test('F06 outcomes distinguish validation, backend empty pages, backend failures and proxy failures', async ({ page }) => {
  await setup(page)
  const normal = backendCapture('students.query'), empty = backendCapture('students.query', 'empty'), failed = backendCapture('students.query', 'snapshot_expired')
  let mode = 'empty', calls = 0
  await page.route('**/console-api/query/api/v1/sicp/students/query', route => {
    calls++
    if (mode === 'proxy') return route.fulfill({ status: 502, contentType: 'text/html', body: '<h1>Bad Gateway</h1>' })
    const fixture = mode === 'empty' ? empty : mode === 'failed' ? failed : normal
    return route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response })
  })
  await page.goto('/console/apis/students.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog'), outcome = dialog.getByRole('status', { name: '测试状态', exact: true })
  await expect(outcome).toHaveAttribute('data-state', 'idle')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill('{broken')
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect(outcome).toHaveAttribute('data-state', 'invalid')
  expect(calls).toBe(0)
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(normal.request))
  for (const [nextMode, state] of [['empty', 'empty'], ['failed', 'business_failed'], ['proxy', 'proxy_failed'], ['success', 'success']]) {
    mode = nextMode!
    await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
    await expect(outcome).toHaveAttribute('data-state', state!)
    if (mode === 'empty') {
      await expect(outcome).toContainText('不表示全部数据为空')
      await expect(dialog.getByLabel('响应原文', { exact: true })).toContainText('"total":12')
    }
    if (mode === 'proxy') {
      await expect(dialog.getByLabel('响应原文', { exact: true })).toHaveText('<h1>Bad Gateway</h1>')
      await expect(dialog.locator('h1')).toHaveCount(0)
    }
  }
  expect(calls).toBe(4)
})

test('F06 outcomes show browser timeout without automatic retry or invented HTTP status', async ({ page }) => {
  await setup(page)
  await page.clock.install()
  let calls = 0, release: (() => void) | undefined
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route('**/console-api/query/api/v1/sicp/students/query', async route => { calls++; await pending; await route.abort().catch(() => {}) })
  await page.goto('/console/apis/students.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog'), outcome = dialog.getByRole('status', { name: '测试状态', exact: true })
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect.poll(() => calls).toBe(1)
  await expect(outcome).toHaveAttribute('data-state', 'waiting')
  await page.clock.runFor(30001)
  await expect(outcome).toHaveAttribute('data-state', 'timeout')
  await expect(dialog).toContainText('未取得 HTTP 响应')
  await expect(dialog.getByRole('region', { name: '响应内容', exact: true })).not.toContainText('HTTP 0')
  expect(calls).toBe(1); release!()
})

for (const [status, available, scenario, expected] of [['succeeded', false, 'success', 'unavailable'], ['succeeded', true, 'empty', 'empty'], ['failed', false, 'success', 'source_failed']] as const) test(`F06 outcomes source ${status} ${scenario} available=${available}`, async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), fixture = sourceJobResult('dwd_bzkscjzlb', status, scenario, available)
  let posts = 0
  await page.route('**/admin/v1/api-tests', route => { posts++; return route.fulfill({ status: accepted.status, headers: { Location: fixture.path }, json: accepted.response }) })
  await page.route(`**${fixture.path}`, route => route.fulfill({ status: fixture.status, body: fixture.raw }))
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog'), outcome = dialog.getByRole('status', { name: '测试状态', exact: true })
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect(outcome).toHaveAttribute('data-state', expected)
  await expect(dialog.getByLabel('管理作业响应原文', { exact: true })).toHaveText(fixture.raw)
  expect(posts).toBe(1)
})

test('F06 outcomes reject changed business paths as protocol failures before any execution', async ({ page }, testInfo) => {
  await setup(page)
  const api = (backendCatalog() as { id: string; path: string }[]).find(api => api.id === 'students.query')!
  // Only catalog routing metadata changes; all fields and records remain the backend fixture.
  await page.route('**/admin/v1/apis/students.query', route => route.fulfill({ json: { code: 'ok', data: { ...api, path: '/admin/v1/api-tests' } } }))
  let calls = 0
  await page.route('**/admin/v1/api-tests', route => { calls++; return route.abort() })
  await page.route('**/console-api/query/api/**', route => { calls++; return route.abort() })
  await page.goto('/console/apis/students.query')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog'), outcome = dialog.getByRole('status', { name: '测试状态', exact: true })
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect(outcome).toHaveAttribute('data-state', 'protocol_failed')
  expect(calls).toBe(0)
  await page.setViewportSize({ width: 390, height: 844 })
  await outcome.scrollIntoViewIfNeeded()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('test-outcome-mobile.png') })
})

for (const operation of ['students.grades.query', 'students.timetable.query', 'organizations.query']) test(`F06 shared views ${operation} switch locally without new requests`, async ({ page }, testInfo) => {
  await setup(page)
  const fixture = backendCapture(operation), api = operations.find(item => item.operation === operation)!
  let calls = 0
  await page.route(`**/console-api/query${api.path}`, route => { calls++; return route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }) })
  await page.goto(`/console/apis/${operation}`)
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(fixture.request))
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  await expect(dialog.getByLabel('响应原文', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '表格', exact: true }).click()
  const table = dialog.getByLabel('本次响应表格', { exact: true })
  await expect(table.locator('tbody tr')).toHaveCount((fixture.response.data as { items: unknown[] }).items.length)
  if (api.kind === 'grades') await expect(table).toContainText('00202600001')
  if (api.kind === 'organizations') {
    await dialog.getByRole('button', { name: '组织树', exact: true }).click()
    await expect(dialog.getByLabel('本次组织树', { exact: true })).toContainText('未核验全量完整性')
    await expect(dialog.getByLabel('本次组织树', { exact: true }).locator('li')).toHaveCount(5)
  }
  if (api.kind === 'timetable') {
    await dialog.getByRole('button', { name: '课表', exact: true }).click()
    await expect(dialog.getByRole('combobox', { name: '上课日期', exact: true })).toHaveValue('2026-09-07')
    await expect(dialog.getByLabel('本次日期课表', { exact: true }).locator('article')).toHaveCount(4)
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await dialog.locator('.response-views').scrollIntoViewIfNeeded()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath(`response-${api.kind}-mobile.png`) })
  await dialog.getByRole('button', { name: '响应头', exact: true }).click()
  await expect(dialog.getByLabel('本次响应头 JSON', { exact: true })).toContainText('X-SICP-Combination')
  await dialog.getByRole('button', { name: 'JSON', exact: true }).click()
  await expect(dialog.getByLabel('响应原文', { exact: true })).toHaveText(JSON.stringify(fixture.response).slice(0, 4096))
  expect(calls).toBe(1)
})

test('F06 shared source views ignore a closed instance late result and recover with GET only', async ({ page }) => {
  await setup(page)
  const accepted = managementCapture('source_test'), fixture = sourceJobResult('dwd_bzkscjzlb')
  let posts = 0, gets = 0, release: (() => void) | undefined, complete: (() => void) | undefined
  const pending = new Promise<void>(resolve => { release = resolve }), completed = new Promise<void>(resolve => { complete = resolve })
  await page.route('**/admin/v1/api-tests', route => { posts++; return route.fulfill({ status: accepted.status, headers: { Location: fixture.path }, json: accepted.response }) })
  await page.route(`**${fixture.path}`, async route => { gets++; if (gets === 1) await pending; await route.fulfill({ status: fixture.status, body: fixture.raw }).catch(() => {}); complete!() })
  await page.goto('/console/apis/dwd_bzkscjzlb')
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '执行源测试', exact: true }).click()
  await expect.poll(() => gets).toBe(1)
  await dialog.getByRole('button', { name: '停止等待并关闭', exact: true }).click()
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  release!(); await completed
  await expect(dialog.getByLabel('本页源数据 JSON', { exact: true })).toHaveCount(0)
  expect(posts).toBe(1); expect(gets).toBe(1)
  await dialog.getByRole('button', { name: '查询原作业', exact: true }).click()
  await expect(dialog.getByLabel('本页源数据 JSON', { exact: true })).toHaveText(fixture.resultText)
  await dialog.getByRole('button', { name: '表格', exact: true }).click()
  await expect(dialog.getByLabel('本次响应表格', { exact: true }).locator('tbody tr')).toHaveCount(JSON.parse(fixture.resultText).list.length)
  await dialog.getByRole('button', { name: '响应头', exact: true }).click()
  await dialog.getByRole('button', { name: 'JSON', exact: true }).click()
  await expect(dialog.getByLabel('管理作业响应原文', { exact: true })).toHaveText(fixture.raw)
  expect(posts).toBe(1); expect(gets).toBe(2)
})

for (const operation of ['students.grades.query', 'students.grades.batch-query']) test(`F06 grade adapter ${operation} keeps submitted scope and backend record semantics`, async ({ page }, testInfo) => {
  await setup(page)
  const fixture = backendCapture(operation), api = operations.find(item => item.operation === operation)!
  let calls = 0
  await page.route(`**/console-api/query${api.path}`, route => { calls++; expect(route.request().postDataJSON()).toEqual(fixture.request); return route.fulfill({ status: fixture.status, headers: fixture.headers, json: fixture.response }) })
  await page.goto(`/console/apis/${operation}`)
  await page.getByRole('button', { name: '在线测试', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify(fixture.request))
  await dialog.getByRole('button', { name: '执行测试', exact: true }).click()
  const scope = dialog.getByRole('region', { name: '本次成绩范围', exact: true })
  await expect(scope).toContainText('跨学年 · 全部学期')
  await expect(scope).toContainText(`共 ${(fixture.response.data as { total: number }).total} 条成绩记录`)
  await scope.getByText('本次条件与成绩口径', { exact: true }).click()
  await expect(scope).toContainText('不同筛选条件取交集，码值数组内部取并集')
  await expect(scope.getByLabel('本次成绩请求条件', { exact: true })).toHaveText(JSON.stringify(fixture.request))
  await dialog.getByRole('button', { name: '表格', exact: true }).click()
  const table = dialog.getByLabel('本次响应表格', { exact: true })
  await expect(table.getByRole('columnheader', { name: /成绩记录学分/ })).toHaveCount(1)
  await expect(table.getByRole('columnheader', { name: /当前行政班名称/ })).toHaveCount(1)
  await expect(table.getByRole('columnheader', { name: /参与记录的源更新时间/ })).toHaveCount(1)
  await expect(table.locator('tbody tr')).toHaveCount((fixture.response.data as { items: unknown[] }).items.length)
  await dialog.getByLabel('请求 JSON', { exact: true }).fill(JSON.stringify({ ...fixture.request, academic_year: '2026-2027' }))
  await expect(scope).toContainText('跨学年 · 全部学期')
  await expect(dialog.getByText('上次读取结果，当前请求尚未取得新响应。', { exact: true })).toBeVisible()
  expect(calls).toBe(1)
  await scope.getByText('本次条件与成绩口径', { exact: true }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await scope.scrollIntoViewIfNeeded()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('grade-context-mobile.png') })
})
