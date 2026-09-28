import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e', fullyParallel: false, workers: 1,
  use: { baseURL: 'http://127.0.0.1:5173/console/', headless: true, launchOptions: { executablePath: process.env.SICP_TEST_BROWSER || undefined } },
  webServer: { command: 'npm run dev', url: 'http://127.0.0.1:5173/console/', reuseExistingServer: !process.env.CI },
})
