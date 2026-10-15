import { test, expect, type Page } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
async function setup(page: Page) {
  const session = managementCapture('session')
  await page.route('**/console-api/session', route => route.fulfill({ json: session.response }))
  for (const name of ['decisions', 'deliveries', 'notification_settings']) {
    const f = managementCapture(name)
    await page.route(`**${f.path}{,?*}`, route => route.fulfill({ json: f.response }))
  }
  const detail = managementCapture('delivery_detail'), id = (detail.response.data as any).delivery.id
  // Select by the actual returned delivery ID, never substitute the illustrative example path's ID.
  await page.route(`**/admin/v1/notifications/${id}`, route => route.fulfill({ json: detail.response }))
}
test('F10 disabled decisions remain visible beside actual unknown recipient delivery and attempts', async ({ page }, info) => {
  await setup(page)
  const writes: string[] = []; page.on('request', request => { if (request.method() !== 'GET') writes.push(request.url()) })
  await page.goto('/console/notifications')
  const decisions = page.getByRole('region', { name: '通知决策', exact: true })
  await expect(decisions).toContainText('通知或该规则未启用')
  await expect(decisions).toContainText('0 / 0 / 0 / 0')
  await page.getByRole('button', { name: '查看投递详情', exact: true }).click()
  const detail = page.getByRole('region', { name: '投递详情', exact: true })
  await expect(detail).toContainText('synthetic@example.invalid')
  await expect(detail).toContainText('发送结果未知')
  await expect(detail).toContainText('结束未确认')
  await expect(detail.getByRole('button', { name: /重发/ })).toHaveCount(0)
  expect(writes).toEqual([])
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('notification-history-mobile.png'), fullPage: true })
})
test('F10 independent notification pagination keeps decisions on delivery failure and rejects detail mismatch', async ({ page }) => {
  await setup(page)
  const first = managementCapture('decisions').response as any; first.data.next_cursor = 'decision-next'
  const urls: URL[] = []
  await page.route('**/admin/v1/notifications/decisions?*', route => {
    const url = new URL(route.request().url()); urls.push(url)
    const response = structuredClone(first)
    if (url.searchParams.has('cursor')) { response.data.items = []; response.data.next_cursor = null }
    return route.fulfill({ json: response })
  })
  await page.goto('/console/notifications')
  await expect(page.getByRole('button', { name: '查看投递详情' })).toBeVisible()
  await page.getByRole('button', { name: '下一批通知决策' }).click()
  expect([...urls[1]!.searchParams.entries()]).toEqual([['cursor', 'decision-next']])
  await page.getByRole('button', { name: '上一批通知决策' }).click()
  await expect(page.getByRole('region', { name: '通知决策', exact: true })).toContainText('通知或该规则未启用')
  const data = managementCapture('delivery_detail').response as any, id = data.data.delivery.id
  data.data.delivery.id = 'mismatched-protocol-id'
  await page.route(`**/admin/v1/notifications/${id}`, route => route.fulfill({ json: data }))
  await page.getByRole('button', { name: '查看投递详情' }).click()
  await expect(page.getByRole('region', { name: '投递详情', exact: true })).toContainText('投递详情标识或尝试记录不符合当前请求')
  await page.route('**/admin/v1/notifications?*', route => route.fulfill({ status: 503, json: { code: 'unavailable' } }))
  await page.getByRole('button', { name: '重新读取逐人投递' }).click()
  await expect(page.getByRole('region', { name: '逐人投递', exact: true })).toContainText('保留已确认批次')
  await expect(page.getByRole('region', { name: '通知决策', exact: true })).toContainText('0 / 0 / 0 / 0')
})
test('F10 mail form validates recorded recipient edits and saves exact string revision without sending mail', async ({ page }) => {
  await setup(page)
  const saved = managementCapture('save_notifications'), address = (managementCapture('deliveries').response.data as any).items[0].recipient
  const writes: { method: string; body: unknown; url: string }[] = []
  await page.route('**/admin/v1/settings/notifications', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: managementCapture('notification_settings').response })
    writes.push({ method: route.request().method(), body: route.request().postDataJSON(), url: route.request().url() })
    return route.fulfill({ json: saved.response })
  })
  let mailCalls = 0
  await page.route('**/admin/v1/notifications/test', route => { mailCalls++; return route.abort() })
  await page.goto('/console/notifications')
  const settings = page.getByRole('region', { name: '邮件通知设置', exact: true })
  await expect(settings).toContainText('配置版本 1')
  await expect(settings.getByLabel('源结构变化', { exact: true })).toBeChecked()
  await settings.getByRole('button', { name: '添加收件人', exact: true }).click()
  await settings.getByLabel('收件人 1', { exact: true }).fill(address)
  await settings.getByRole('button', { name: '添加收件人', exact: true }).click()
  await settings.getByLabel('收件人 2', { exact: true }).fill(address)
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings.getByRole('alert')).toContainText('收件人重复')
  expect(writes).toEqual([])
  await settings.getByRole('button', { name: '删除收件人 2', exact: true }).click()
  await settings.getByLabel('收件人 1', { exact: true }).fill(address.replace('@', ''))
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings.getByRole('alert')).toContainText('完整邮箱地址')
  await settings.getByRole('button', { name: '删除收件人 1', exact: true }).click()
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings).toContainText('配置版本 2')
  expect(writes).toHaveLength(1); expect(writes[0]!.method).toBe('PUT'); expect(writes[0]!.body).toEqual(saved.request)
  expect(mailCalls).toBe(0)
})
test('F10 settings conflict preserves local edits and compares actual server settings before an explicit choice', async ({ page }) => {
  await setup(page)
  let reads = 0, writes = 0
  await page.route('**/admin/v1/settings/notifications', route => {
    if (route.request().method() === 'PUT') { writes++; return route.fulfill({ status: 409, json: { code: 'revision_conflict' } }) }
    reads++
    const response = managementCapture('notification_settings').response as any
    if (reads > 1) response.data.settings = managementCapture('save_notifications').response.data
    return route.fulfill({ json: response })
  })
  await page.goto('/console/notifications')
  const settings = page.getByRole('region', { name: '邮件通知设置', exact: true })
  await settings.getByLabel('源结构变化', { exact: true }).uncheck()
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings).toContainText('本地编辑仍保留')
  await expect(settings.getByLabel('源结构变化', { exact: true })).not.toBeChecked()
  await expect(settings.getByRole('button', { name: '保存邮件配置', exact: true })).toBeDisabled()
  await settings.getByRole('button', { name: '读取邮件配置', exact: true }).click()
  await expect(settings).toContainText('版本 2')
  await expect(settings).toContainText('源结构变化：开启')
  await expect(settings.getByLabel('源结构变化', { exact: true })).not.toBeChecked()
  await settings.getByRole('button', { name: '使用此版本，保留本地编辑', exact: true }).click()
  await expect(settings).toContainText('配置版本 2')
  await expect(settings.getByLabel('源结构变化', { exact: true })).not.toBeChecked()
  expect(writes).toBe(1)
  await settings.getByRole('button', { name: '读取邮件配置', exact: true }).click()
  await settings.getByRole('button', { name: '采用服务端配置，放弃本地编辑', exact: true }).click()
  await expect(settings.getByRole('alert')).toHaveCount(0)
  await expect(settings.getByLabel('源结构变化', { exact: true })).toBeChecked()
  expect(writes).toBe(1)
})

test('F10 unconfigured channel blocks enabling but clearing recipients can save disabled without blocking history', async ({ page }) => {
  await setup(page)
  const address = (managementCapture('deliveries').response.data as any).items[0].recipient, saved = managementCapture('save_notifications')
  let writes = 0
  await page.route('**/admin/v1/settings/notifications', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: managementCapture('notification_settings').response })
    writes++; expect(route.request().postDataJSON()).toEqual(saved.request)
    return route.fulfill({ json: saved.response })
  })
  await page.goto('/console/notifications')
  const settings = page.getByRole('region', { name: '邮件通知设置', exact: true })
  await settings.getByLabel('启用邮件通知', { exact: true }).check()
  await settings.getByRole('button', { name: '添加收件人', exact: true }).click()
  await settings.getByLabel('收件人 1', { exact: true }).fill(address)
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings.getByRole('alert')).toContainText('邮件通道未配置')
  expect(writes).toBe(0)
  await expect(page.getByRole('region', { name: '通知决策', exact: true })).toContainText('0 / 0 / 0 / 0')
  await settings.getByRole('button', { name: '删除收件人 1', exact: true }).click()
  await expect(settings).toContainText('本次将保存为关闭通知')
  await settings.getByRole('button', { name: '保存邮件配置', exact: true }).click()
  await expect(settings.getByLabel('启用邮件通知', { exact: true })).not.toBeChecked()
  await expect(settings).toContainText('配置版本 2')
  expect(writes).toBe(1)
})
async function configuredMail(page: Page) {
  await setup(page)
  // Protocol eligibility variant only: reuse the recorded recipient, revision and actual job DTO.
  const config = managementCapture('notification_settings').response as any
  config.data.settings.revision = (managementCapture('test_notifications').request as any).revision
  config.data.settings.enabled = true
  config.data.settings.recipients = [(managementCapture('deliveries').response.data as any).items[0].recipient]
  config.data.channel_state = 'configured'; config.data.channel_diagnostics.configured = true
  await page.route('**/admin/v1/settings/notifications', route => route.fulfill({ json: config }))
  const job = managementCapture('notification_test_job')
  await page.route(`**${job.path}`, route => route.fulfill({ json: job.response }))
}
test('F10 explicit mail locks saved revision and recovers the same key after response loss without storing recipients', async ({ page }) => {
  await configuredMail(page)
  const posts: any[] = [], accepted = managementCapture('test_notifications')
  await page.route('**/admin/v1/notifications/test', route => {
    posts.push(route.request().postDataJSON())
    return posts.length === 1 ? route.abort('failed') : route.fulfill({ status: 202, json: accepted.response })
  })
  await page.goto('/console/notifications')
  const mail = page.getByRole('region', { name: '测试邮件', exact: true })
  await mail.getByRole('button', { name: '发送测试邮件', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '确认发送测试邮件', exact: true })
  await expect(dialog).toContainText('synthetic@example.invalid')
  expect(posts).toHaveLength(0)
  await dialog.getByRole('button', { name: '确认发送测试邮件', exact: true }).click()
  await expect(dialog).toContainText('受理结果未确认')
  const recovery = await page.evaluate(() => sessionStorage.getItem('sicp.pending-operations.v1'))
  expect(recovery).not.toContain('synthetic@example.invalid')
  expect(Object.keys(posts[0]).sort()).toEqual(['idempotency_key', 'revision'])
  await page.reload()
  await expect(mail).toContainText('已恢复原测试请求')
  expect(posts).toHaveLength(1)
  await mail.getByRole('button', { name: '原键重试测试请求' }).click()
  await dialog.getByRole('button', { name: '确认发送测试邮件', exact: true }).click()
  await expect(mail).toContainText('测试作业已受理，不代表邮件已送达')
  expect(posts).toHaveLength(2); expect(posts[1]).toEqual(posts[0])
  await expect(mail).toContainText((accepted.response.data as any).id)
  await expect(mail.getByRole('button', { name: '查看投递详情' })).toHaveCount(0)
  await expect(mail).toContainText('仅展示本批中与此测试事实号完全一致')
  await page.reload()
  await expect(mail).toContainText((accepted.response.data as any).id)
  expect(posts).toHaveLength(2)
})
test('F10 mail testing is disabled for unconfigured or unsaved settings and cancel does not send', async ({ page }) => {
  await setup(page); await page.goto('/console/notifications')
  await expect(page.getByRole('button', { name: '发送测试邮件', exact: true })).toBeDisabled()
  await configuredMail(page); await page.reload()
  const send = page.getByRole('button', { name: '发送测试邮件', exact: true })
  await expect(send).toBeEnabled()
  await page.getByLabel('源结构变化', { exact: true }).uncheck()
  await expect(send).toBeDisabled()
  await page.getByLabel('源结构变化', { exact: true }).check()
  let posts = 0
  await page.route('**/admin/v1/notifications/test', route => { posts++; return route.abort() })
  await send.click()
  await page.getByRole('dialog').getByRole('button', { name: '暂不提交' }).click()
  await expect(send).toBeEnabled(); expect(posts).toBe(0)
})
test('F10 delivery protocol states never imply inbox arrival or automatic resend of an unknown result', async ({ page }) => {
  await setup(page)
  const detail = managementCapture('delivery_detail').response as any, id = detail.data.delivery.id
  let state = 'unknown'
  await page.route(`**/admin/v1/notifications/${id}`, route => {
    const body = structuredClone(detail)
    // State transitions are protocol variants of the original recipient, not invented business records.
    body.data.delivery.status = state; body.data.attempts[0].outcome = state
    return route.fulfill({ json: body })
  })
  await page.goto('/console/notifications')
  await page.getByRole('button', { name: '查看投递详情' }).click()
  const panel = page.getByRole('region', { name: '投递详情', exact: true })
  const meanings: Record<string, string> = { queued: '尚未开始发送', retry: '服务端已安排重试', sending: '通道调用进行中', accepted: '不代表收件箱已送达', rejected: '通道明确拒绝', unknown: '可能已被通道处理', cancelled: '不表示已发送邮件被撤回', exhausted: '重试次数或有效期限' }
  for (const [value, text] of Object.entries(meanings)) {
    state = value; await panel.getByRole('button', { name: '刷新投递详情' }).click()
    await expect(panel).toContainText(text)
    await expect(panel).toContainText('第 1 次')
    await expect(panel.getByRole('button', { name: /重发/ })).toHaveCount(0)
  }
  await expect(panel).toContainText('其他收件人的成功或失败分别记录')
})