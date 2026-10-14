import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { backendCapture } from '../fixtures/backend'
import { operations, queryBody, cell } from '../src/queryOperations'

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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '业务查询' }).click()
  await page.getByLabel('学号 *', { exact: true }).fill('001')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByRole('heading', { name: '登录管理控制台' })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('登录已过期')
  await expect(page.getByRole('navigation')).toHaveCount(0)
})

async function setup(page: Page) {
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
  await page.getByRole('link', { name: '运行总览' }).click()
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
  await page.getByRole('link', { name: '业务查询' }).click()
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
  await page.getByRole('link', { name: '业务查询', exact: true }).click()
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  const beforeLeaving = checks
  await page.clock.runFor(20100)
  expect(checks).toBe(beforeLeaving)
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
  await page.getByRole('link', { name: '更新任务' }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  await page.getByRole('link', { name: '业务查询' }).click()
  await page.getByLabel('学号 *', { exact: true }).fill('001')
  await page.getByRole('button', { name: '查询数据' }).click()
  await expect(page.getByRole('alert')).toContainText('denied-id')
  await page.screenshot({ path: testInfo.outputPath('query-mobile.png'), fullPage: true })
  for (const title of ['运行总览', '业务查询', '更新任务']) {
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
    await page.getByRole('link', { name: '业务查询' }).click()
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
      await page.getByRole('link', { name: '业务查询', exact: true }).click()
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
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
    await page.getByRole('button', { name: /slow-old-task/ }).click()
    await seen
    await page.getByRole('link', { name: '业务查询', exact: true }).click()
    await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
    await page.getByRole('link', { name: '业务查询', exact: true }).click()
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
  await page.getByRole('link', { name: '业务查询', exact: true }).click()
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
  await page.getByRole('link', { name: '业务查询', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/queries$/)
  await page.reload()
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/tasks$/)
  await page.goBack()
  await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
  await page.goto('./#tasks')
  await expect(page).toHaveURL(/\/console\/tasks$/)
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
    await page.getByRole('link', { name: '业务查询', exact: true }).click()
    await page.getByRole('button', { name: '学生信息', exact: true }).click()
    await page.getByRole('button', { name: '查询数据', exact: true }).click()
    await seen
    await page.getByRole('link', { name: '运行总览', exact: true }).click()
    release()
    await expect.poll(() => pendingRequest.failure()).not.toBeNull()
    await page.getByRole('link', { name: '业务查询', exact: true }).click()
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
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
    await page.getByRole('button', { name: '强制全量更新', exact: true }).click()
    await page.getByRole('button', { name: '确认提交更新' }).click()
    await seen
    await page.getByRole('link', { name: '业务查询', exact: true }).click()
    await expect(page.getByRole('heading', { name: '业务查询', exact: true })).toBeVisible()
    release()
    await expect.poll(() => pendingRequest.failure()).not.toBeNull()
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
    await page.getByRole('link', { name: '更新任务', exact: true }).click()
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
  const queryLink = page.getByRole('link', { name: '业务查询', exact: true })
  await queryLink.focus()
  await page.keyboard.press('Enter')
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
