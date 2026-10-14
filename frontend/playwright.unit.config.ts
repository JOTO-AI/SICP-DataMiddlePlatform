import { defineConfig } from '@playwright/test'

// Pure protocol/state tests need neither a browser nor the Vite web server.
export default defineConfig({ testDir: './unit', workers: 1, timeout: 10000 })
