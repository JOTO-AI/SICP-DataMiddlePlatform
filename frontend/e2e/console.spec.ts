import { test, expect, type Page } from '@playwright/test'

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
    return route.fulfill({ json: { code: 'accepted', data: { task_id: 'repair-one-table' } } })
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
  await page.getByRole('checkbox').check()
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
  await expect(progress).toContainText('首次初始化完成前，成绩与课表暂不可查询。')
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
  await expect(page.getByText('初始化失败，可重新从上游完整采集并初始化，无需重启服务；上游冷却期间任务会等待。')).toBeVisible()
  await expect(page.getByText(/重启后系统会恢复未完成的初始化/)).toBeVisible()
  await expect(page.getByRole('radio')).toHaveCount(2)
  await expect(page.getByRole('radio', { name: /首次初始化|增量同步/ })).toHaveCount(0)
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
  await expect(page.getByText('无法确认当前任务状态，请先刷新记录。')).toBeVisible()
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
