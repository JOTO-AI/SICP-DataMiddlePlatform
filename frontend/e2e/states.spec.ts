import { test, expect } from '@playwright/test'
function ratio(a: string, b: string) {
  const luminance = (color: string) => { const [r, g, blue] = color.match(/[\d.]+/g)!.slice(0, 3).map(value => { const v = Number(value) / 255; return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4 }); return r! * .2126 + g! * .7152 + blue! * .0722 }
  const x = luminance(a), y = luminance(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05)
}
test('F02 control states retain contrast, focus, disabled behavior and reduced motion', async ({ page }, testInfo) => {
  await page.goto('/console/e2e/states.html')
  const input = page.getByRole('textbox', { name: '输入', exact: true })
  const submit = page.getByRole('button', { name: '读取数据', exact: true })
  async function contrast(selector: string, pseudo?: string) {
    const colors = await page.locator(selector).evaluate((element, pseudo) => {
      const style = getComputedStyle(element, pseudo), own = getComputedStyle(element)
      let background = own.backgroundColor, parent = element.parentElement
      while (background === 'rgba(0, 0, 0, 0)' && parent) { background = getComputedStyle(parent).backgroundColor; parent = parent.parentElement }
      return { foreground: style.color, background, opacity: own.opacity, border: own.borderTopColor }
    }, pseudo)
    expect(colors.opacity).toBe('1'); expect(ratio(colors.foreground, colors.background)).toBeGreaterThanOrEqual(4.5)
    return colors
  }
  await contrast('button.primary')
  await submit.hover(); await contrast('button.primary')
  await contrast('input[placeholder]', '::placeholder')
  await contrast('.update-progress .hint')
  await contrast('.error'); await contrast('.warning')
  await input.focus(); await expect(input).toBeFocused()
  expect(await input.evaluate(element => getComputedStyle(element).outlineStyle)).toBe('solid')
  await page.keyboard.press('Tab'); await expect(page.getByRole('combobox')).toBeFocused()
  const invalid = page.getByRole('textbox', { name: '错误输入' })
  await invalid.hover()
  expect(await invalid.evaluate(element => getComputedStyle(element).borderTopColor)).toBe('rgb(217, 45, 32)')
  await submit.click(); await expect(page.getByRole('status')).toHaveText('提交次数 1')
  await page.getByRole('button', { name: '切换加载' }).click()
  for (const control of await page.locator('fieldset input, fieldset select, fieldset textarea, fieldset button').all()) await expect(control).toBeDisabled()
  await contrast('button.primary'); const disabled = await contrast('input[placeholder]', '::placeholder')
  expect(ratio(disabled.border, disabled.background)).toBeGreaterThanOrEqual(3)
  await expect(page.getByRole('form')).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('status')).toHaveText('提交次数 1')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  expect(await page.locator('button.primary').evaluate(element => getComputedStyle(element).transitionDuration)).toBe('0s')
  await page.screenshot({ path: testInfo.outputPath('disabled-controls.png'), fullPage: true })
})
