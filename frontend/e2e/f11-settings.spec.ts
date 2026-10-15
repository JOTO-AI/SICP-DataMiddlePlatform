import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
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