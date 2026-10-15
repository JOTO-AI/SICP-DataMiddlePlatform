import { defineConfig } from '@playwright/test'
import base from './playwright.config'

// Capacity thresholds were registered for an isolated single worker, not a parallel functional run.
export default defineConfig({
  ...base,
  testDir: './performance', fullyParallel: false, workers: 1,
  outputDir: '../artifacts/frontend-capacity/results',
  reporter: [['list'], ['json', { outputFile: '../artifacts/frontend-capacity/report.json' }]],
})