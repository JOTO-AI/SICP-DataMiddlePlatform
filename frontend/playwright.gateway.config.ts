import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './gateway-e2e', timeout: 900_000, workers: 1, retries: 0,
  reporter: [['list'], ['json', { outputFile: '../artifacts/gateway/browser.json' }]],
  outputDir: '../artifacts/gateway/browser-results',
  use: { browserName: 'chromium', trace: 'off', screenshot: 'off', video: 'off' },
})
