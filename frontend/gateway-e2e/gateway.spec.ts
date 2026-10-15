import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { loadConfigFromFile } from 'vite'
const root = resolve(process.cwd(), '..')
const origin = (tls: boolean) => `${tls ? 'https' : 'http'}://sicp-gateway.test:28080`
function operation(action: string) {
  try { execFileSync('python3', ['scripts/gateway_deployment_checks.py', action], { cwd: root, timeout: 600_000, stdio: 'pipe' }) }
  catch (error) {
    const detail = (error as { stdout?: Buffer }).stdout?.toString() || "no fixture diagnostic"
    throw new Error(`gateway operation failed: ${action}: ${detail}`)
  }
}
async function login(page: Page, tls: boolean, base = origin(tls)) {
  await page.goto(`${base}/console/`)
  await page.getByLabel('管理员账号').fill('synthetic-admin')
  await page.getByLabel('密码', { exact: true }).fill('synthetic-admin-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeVisible()
  expect((await page.context().cookies()).some(cookie => cookie.httpOnly && cookie.secure === tls)).toBe(true)
  const result = await page.evaluate(async () => {
    const session = await (await fetch('/console-api/session')).json()
    const headers = { 'Content-Type': 'application/json', 'X-SICP-Request': 'console', 'X-SICP-CSRF': session.data.csrf }
    const query = await fetch('/console-api/query/api/v1/sicp/students/grades/query', {
      method: 'POST', headers,
      body: JSON.stringify({ student_number: '00202600001', academic_year: '2025-2026', page: 1, page_size: 100 }),
    })
    const bad = await fetch('/admin/v1/tables/not-a-table/force-full', {
      method: 'POST', headers: { ...headers, 'X-SICP-CSRF': 'wrong' }, body: '{}',
    })
    const good = await fetch('/admin/v1/tables/not-a-table/force-full', {
      method: 'POST', headers: { ...headers, 'Idempotency-Key': 'gateway-browser-fixture' }, body: '{}',
    })
    return { query: query.status, bad: bad.status, good: good.status }
  })
  expect(result.query).toBe(200)
  expect(result.bad).toBe(403)
  expect([400, 404]).toContain(result.good)
  await page.getByRole('link', { name: '更新事件', exact: true }).click()
  await expect(page.getByRole('heading', { name: '更新任务', exact: true })).toBeVisible()
}
test('GW-I03/GW-I05 real sessions, HTTP → HTTPS → HTTP, renewal and recovery', async ({ browser }) => {
  const kept = await browser.newContext()
  const page = await kept.newPage()
  await login(page, false)
  operation('https')
  operation('smoke-https')
  await login(page, true)
  const second = await browser.newContext()
  const logoutPage = await second.newPage()
  await login(logoutPage, true)
  await logoutPage.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(logoutPage.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  operation('bad-preflight')
  operation('renew')
  await page.reload()
  await expect(page.getByRole('button', { name: '退出登录', exact: true })).toBeVisible()
  operation('http')
  expect((await kept.cookies(origin(true))).some(cookie => cookie.secure)).toBe(true)
  await page.goto(`${origin(false)}/console/`)
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
  await kept.clearCookies()
  await login(page, false)
  await login(logoutPage, false)
  operation('failed-switch')
  await kept.clearCookies()
  await login(page, false)
  await kept.close()
  await second.close()
})
test('GW-I03 Vite HTTP proxy reaches the real unified gateway', async ({ page }) => {
  const config = await loadConfigFromFile({ command: 'serve', mode: 'test' }, 'vite.config.ts')
  expect(config?.config.server?.proxy?.['/console-api']).toMatchObject({ target: 'http://127.0.0.1:18080' })
  await login(page, false, 'http://sicp-gateway.test:5173')
})
