import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
test('F12 update deep link preserves control facts beside missing or malformed diagnostics without triggering work', async ({ page }) => {
  await page.route('**/console-api/session', route => route.fulfill({ json: managementCapture('session').response }))
  const detail = managementCapture('update_detail'), response = detail.response as any
  response.data.diagnostics_unavailable = true
  response.data.acquisition = { tables: null } // Diagnostic protocol failure must not erase actual queued task facts.
  await page.route('**' + detail.path, route => route.fulfill({ json: response }))
  const nodes = managementCapture('operation_nodes')
  await page.route('**/admin/v1/update-events/*/operations?*', route => route.fulfill({ json: nodes.response }))
  const writes: string[] = []; page.on('request', request => { if (request.method() !== 'GET') writes.push(request.url()) })
  await page.goto('/console/updates/' + response.data.task_id)
  await expect(page.getByRole('region', { name: '更新事件事实' })).toContainText(response.data.task_id)
  const diagnostic = page.getByRole('region', { name: '任务执行与诊断' })
  await expect(diagnostic).toContainText('部分诊断字段不完整')
  await expect(diagnostic).toContainText('暂时无法读取调用日志，任务状态不受影响')
  await expect(diagnostic.getByRole('link')).toHaveAttribute('href', '/console/updates')
  const raw = page.getByLabel('更新事件响应原文', { exact: true })
  await expect(raw).toHaveCount(0)
  await page.getByText('实际任务响应', { exact: true }).click()
  await expect(raw).toBeVisible()
  await page.getByText('实际任务响应', { exact: true }).click()
  await expect(raw).toHaveCount(0)
  expect(writes).toEqual([])
})