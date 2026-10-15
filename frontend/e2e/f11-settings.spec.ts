import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
test.beforeEach(async ({ page }) => { await page.route('**/admin/v1/settings/retention', route => route.fulfill({ json: managementCapture('retention').response })) })
test('F11 system selected backend fields retain unavailable and unknown instead of zero or healthy', async ({ page }, info) => {
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  await page.route('**/admin/v1/system/status', route => route.fulfill({ json: managementCapture('system').response }))
  await page.goto('/console/settings')
  const panel = page.getByRole('region', { name: '系统运行状态', exact: true })
  await expect(panel).toContainText('双源独立存储')
  await expect(panel).toContainText('保留期读取：不可用')
  await expect(panel).toContainText('探测时间：未提供')
  for (const source of ['平台日志', '上游日志']) {
    const data = page.getByRole('region', { name: source + '存储状态' })
    await expect(data).toContainText('缓冲已用字节 / 文件未确认 / 未确认')
    await expect(data).toContainText('事件 / 回执实际字节未确认 / 未确认')
  }
  await page.route('**/admin/v1/system/status', route => route.fulfill({ status: 503, json: { code: 'unavailable' } }))
  await panel.getByRole('button', { name: '刷新系统状态' }).click()
  await expect(panel).toContainText('上次已确认快照')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('system-mobile.png'), fullPage: true })
})
test('F11 runtime settings link to supported pages while deployment secrets have no editor or raw echo', async ({ page }) => {
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  const response = managementCapture('system').response as any
  // Security boundary mutation, never a normal fixture or real credential.
  response.data.credentials = { source_configured: false, smtp_configured: true, log_cursor_configured: false, password: 'DO-NOT-RENDER-secret' }
  response.data.smtp_reachability = 'unknown'
  await page.route('**/admin/v1/system/status', route => route.fulfill({ json: response }))
  await page.goto('/console/settings')
  const config = page.getByRole('region', { name: '配置管理范围' })
  await expect(config.getByRole('link', { name: '日志保留期' })).toHaveAttribute('href', '/console/settings/retention')
  await expect(config.getByRole('link', { name: '邮件通知配置' })).toHaveAttribute('href', '/console/notifications')
  await expect(config).toContainText('连接限制（部署时设置）')
  await expect(config).toContainText('SMTP 配置已提供')
  await expect(config).toContainText('SMTP 可达性未知')
  await expect(config.locator('input,textarea,select')).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('DO-NOT-RENDER-secret')
})
test('F11 retention validates days, preserves old confirmed values on conflict and saves captured payload without cleanup', async ({ page }) => {
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  await page.route('**/admin/v1/system/status', route => route.fulfill({ json: managementCapture('system').response }))
  let writes = 0, cleanups = 0
  await page.route('**/admin/v1/maintenance/**', route => { cleanups++; return route.abort() })
  await page.route('**/admin/v1/settings/retention', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: managementCapture('retention').response })
    writes++
    if (writes === 1) return route.fulfill({ status: 409, json: { code: 'revision_conflict' } })
    expect(route.request().postDataJSON()).toEqual(managementCapture('save_retention').request)
    return route.fulfill({ json: managementCapture('save_retention').response })
  })
  await page.goto('/console/settings/retention')
  const panel = page.getByRole('region', { name: '日志保留期', exact: true }), input = panel.getByLabel('平台日志保留天数')
  await input.fill('3651'); await panel.getByRole('button', { name: '保存保留期' }).click()
  await expect(panel.getByRole('alert')).toContainText('1 至 3650'); expect(writes).toBe(0)
  await input.fill('1'); await panel.getByRole('button', { name: '保存保留期' }).click()
  await expect(panel).toContainText('上次确认配置：版本 1 · 平台 30 天')
  await expect(input).toHaveValue('1')
  await expect(panel.getByRole('button', { name: '保存保留期' })).toBeDisabled()
  await panel.getByRole('button', { name: '读取保留期' }).click()
  await panel.getByRole('button', { name: '保留编辑，使用最新版本' }).click()
  await expect(input).toHaveValue('1')
  await input.fill('30'); await panel.getByRole('button', { name: '保存保留期' }).click()
  await expect(panel).toContainText('上次确认配置：版本 2')
  expect(writes).toBe(2); expect(cleanups).toBe(0)
})
async function cleanupSetup(page: import('@playwright/test').Page) {
  await page.clock.setFixedTime(new Date('2026-10-13T00:01:00Z'))
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  await page.route('**/admin/v1/system/status', route => route.fulfill({ json: managementCapture('system').response }))
  await page.goto('/console/settings')
}
test('F11 cleanup binds one source, keeps zero preview inert, and requires new preview for expiration or changed policy', async ({ page }) => {
  await cleanupSetup(page)
  let previews = 0, submissions = 0
  await page.route('**/admin/v1/maintenance/cleanup', route => { submissions++; return route.abort() })
  await page.route('**/admin/v1/maintenance/cleanup/preview', route => {
    previews++
    const value = managementCapture('preview').response as any
    expect(Object.keys(route.request().postDataJSON())).toEqual(['source'])
    value.data.source = route.request().postDataJSON().source
    // Nonzero range is an explicit preview-boundary variant, not a new ordinary log fixture.
    if (previews > 1) { value.data.count = '1'; value.data.through = '1' }
    return route.fulfill({ json: value })
  })
  const panel = page.getByRole('region', { name: '日志清理', exact: true })
  await panel.getByRole('button', { name: '预览所选源' }).click()
  await expect(panel).toContainText('本次没有可清理记录')
  await expect(panel.getByRole('button', { name: '确认清理此源' })).toBeDisabled()
  await panel.getByRole('combobox', { name: '清理日志源', exact: true }).selectOption('upstream')
  await expect(panel.getByRole('region', { name: '清理预览' })).toHaveCount(0)
  await panel.getByRole('button', { name: '预览所选源' }).click()
  await expect(panel).toContainText('上游日志预览')
  await page.clock.setFixedTime(new Date('2026-10-13T00:11:00Z'))
  await expect(panel).toContainText('预览已过期')
  await expect(panel.getByRole('button', { name: '确认清理此源' })).toBeDisabled()
  await page.clock.setFixedTime(new Date('2026-10-13T00:01:00Z'))
  await panel.getByRole('button', { name: '预览所选源' }).click()
  await page.route('**/admin/v1/settings/retention', route => route.fulfill({ json: managementCapture('save_retention').response }))
  await page.getByRole('button', { name: '读取保留期' }).click()
  await expect(panel).toContainText('保留策略版本已变化')
  await expect(panel.getByRole('button', { name: '确认清理此源' })).toBeDisabled()
  expect(submissions).toBe(0)
})
test('F11 cleanup explicit confirmation sends only preview revision and idempotency key, then displays actual accepted job', async ({ page }) => {
  await cleanupSetup(page)
  const preview = managementCapture('preview').response as any
  preview.data.count = '1'; preview.data.through = '1' // Preview quantity boundary only.
  await page.route('**/admin/v1/maintenance/cleanup/preview', route => route.fulfill({ json: preview }))
  const bodies: any[] = []
  await page.route('**/admin/v1/maintenance/cleanup', route => { bodies.push(route.request().postDataJSON()); return route.fulfill({ status: 202, json: managementCapture('cleanup').response }) })
  const panel = page.getByRole('region', { name: '日志清理', exact: true })
  await panel.getByRole('button', { name: '预览所选源' }).click()
  await panel.getByRole('button', { name: '确认清理此源' }).click()
  expect(bodies).toHaveLength(0)
  const dialog = page.getByRole('dialog', { name: '确认日志清理' })
  await expect(dialog).toContainText('平台日志')
  await dialog.getByRole('button', { name: '提交此源清理' }).click()
  await expect(panel).toContainText('清理作业已受理，实际删除结果尚需确认')
  expect(bodies).toHaveLength(1)
  expect(bodies[0]).toEqual({ preview_id: preview.data.id, revision: preview.data.revision, idempotency_key: expect.any(String) })
  await expect(panel.getByRole('region', { name: '清理作业', exact: true })).toContainText((managementCapture('cleanup').response.data as any).id)
  await expect(panel.getByRole('combobox')).toBeDisabled()
})