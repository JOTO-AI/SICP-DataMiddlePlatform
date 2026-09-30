import { test, expect, type Page } from '@playwright/test'

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
  await expect(page.getByRole('checkbox')).toBeDisabled()
})

test('automatic tasks are discoverable without initialization or sync controls', async ({ page }, testInfo) => {
  await setup(page)
  await page.route('**/admin/v1/tasks/', route => route.fulfill({ json: { code: 'ok', data: { initialized: false, interval_seconds: 7200, items: [{ task_id: 'automatic-initial-task', mode: 'initialize', status: 'failed', operator: 'scheduler', accepted_at: '2026-09-28T10:00:00Z' }] } } }))
  await page.route('**/admin/v1/tasks/automatic-initial-task', route => route.fulfill({ json: { code: 'ok', data: { task_id: 'automatic-initial-task', mode: 'initialize', status: 'failed', operator: 'scheduler', accepted_at: '2026-09-28T10:00:00Z', failure: 'acquisition_failed' } } }))
  await page.getByRole('link', { name: '更新任务', exact: true }).click()
  await expect(page.getByRole('button', { name: '强制全量更新', exact: true })).toBeDisabled()
  await expect(page.getByText('强制全量更新在首次初始化完成后可用。')).toBeVisible()
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
