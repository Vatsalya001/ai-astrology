import { defineConfig, devices } from '@playwright/test'

/**
 * End-to-end tests.
 *
 * These are the only tests that exercise the web app against a real API.
 * Everything else stubs at a boundary: the Go tests use httptest, the
 * Python tests use a test client, and the web build only typechecks.
 * If the contract between browser and API breaks, this is what notices.
 *
 * The stack must already be running (`task up` + `task dev`). Playwright
 * is deliberately NOT configured to start it: with four services across
 * three languages, a webServer block that half-starts things produces
 * failures that look like test failures and are not.
 */
export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,

  // Clears rate-limit windows so a run does not inherit the previous
  // one's. See the file for why this resets state rather than limits.
  globalSetup: './tests/e2e/global-setup.ts',

  // A test marked .only is almost always a debugging leftover. Failing
  // the build is better than silently running one test in CI.
  forbidOnly: !!process.env.CI,

  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',

  use: {
    baseURL: process.env.WEB_URL ?? 'http://localhost:3000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
})
