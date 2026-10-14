import { readFileSync } from 'node:fs'
const css = readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8')
const colors = Object.fromEntries([...css.matchAll(/--(color-[\w-]+):\s*(#[\da-f]{6});/gi)].map(match => [match[1], match[2]]))
const luminance = hex => {
  const rgb = hex.slice(1).match(/../g).map(value => parseInt(value, 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4)
  return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722
}
const pairs = [
  ['color-text', 'color-app-bg', 4.5], ['color-text', 'color-panel', 4.5],
  ['color-text-muted', 'color-app-bg', 4.5], ['color-text-muted', 'color-panel', 4.5], ['color-text-muted', 'color-surface-muted', 4.5],
  ['color-placeholder', 'color-panel', 4.5], ['color-panel', 'color-primary', 4.5], ['color-panel', 'color-primary-hover', 4.5],
  ['color-primary', 'color-panel', 4.5], ['color-primary-text', 'color-primary-soft', 4.5], ['color-text-on-tint', 'color-primary-soft', 4.5],
  ['color-success-text', 'color-success-soft', 4.5], ['color-danger', 'color-danger-soft', 4.5], ['color-warning', 'color-warning-soft', 4.5],
  ['color-disabled-text', 'color-disabled-bg', 4.5], ['color-placeholder', 'color-surface-muted', 4.5],
  ['color-control-border', 'color-disabled-bg', 3], ['color-danger', 'color-panel', 3],
  ['color-control-border', 'color-panel', 3], ['color-focus', 'color-app-bg', 3],
]
for (const [foreground, background, required] of pairs) {
  const a = luminance(colors[foreground]), b = luminance(colors[background]), ratio = (Math.max(a, b) + .05) / (Math.min(a, b) + .05)
  if (ratio < required) throw new Error(`${foreground} / ${background}: ${ratio.toFixed(2)} < ${required}`)
  console.log(`${foreground} / ${background}: ${ratio.toFixed(2)}:1`)
}
console.log(`F02 theme PASS: ${pairs.length} semantic color pairs meet text/control thresholds.`)
