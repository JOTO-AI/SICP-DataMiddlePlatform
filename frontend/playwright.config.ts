import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e', fullyParallel: false, workers: 1,
  use: { baseURL: 'http://127.0.0.1:5173/console/', headless: true, launchOptions: { executablePath: process.env.SICP_TEST_BROWSER || undefined } },
  projects: [
    { name: 'loopback' },
    { name: 'intranet-http', use: {
      baseURL: 'http://sicp-console.test:5173/console/',
      launchOptions: { executablePath: process.env.SICP_TEST_BROWSER || undefined, args: ['--host-resolver-rules=MAP sicp-console.test 127.0.0.1', '--no-proxy-server'] },
    } },
  ],
  webServer: { command: 'npm run dev', url: 'http://127.0.0.1:5173/console/', reuseExistingServer: !process.env.CI, env: { __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS: 'sicp-console.test' } },
})
