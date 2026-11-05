import { defineConfig, devices } from '@playwright/test'

// Browser tests against the running compose stack. STACK_URL names the
// stack, as it does for the Go stack tests; the generated certificate is not
// trusted, so TLS errors are ignored.
export default defineConfig({
  testDir: 'tests/e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.STACK_URL ?? 'https://ocis.localhost',
    ignoreHTTPSErrors: true,
    headless: true,
    trace: 'retain-on-failure'
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }]
})
