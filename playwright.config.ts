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

  /*
    Visual comparison tolerance.

    Not zero. Anti-aliasing of the chart's diagonals differs by a pixel
    or two between machines and between driver versions, and a suite that
    fails on that is a suite people learn to re-baseline without looking
    — which is strictly worse than not having one.

    maxDiffPixelRatio rather than maxDiffPixels, so the allowance scales
    with the viewport instead of being generous on a phone and strict on
    a desktop. 0.2% of a 360x780 screen is about 560 pixels: far more
    than anti-aliasing, far less than a missing background or a
    collapsed grid.
  */
  expect: {
    toHaveScreenshot: {
      maxDiffPixelRatio: 0.002,

      /*
        0.03, not Playwright's default 0.2.

        `threshold` is a perceived-colour distance in YIQ space, and this
        product's entire palette lives in a narrow band of very dark
        navy. Meaningful differences here are SMALL in that space: the
        chart's `surface` background (#0B1026) against the black an SVG
        falls back to when its fill class is dropped is a distance of
        0.067 — so at the default, Playwright counts those two pixels as
        identical and the suite passes with the chart's background gone.

        That is not hypothetical. It is precisely the defect
        `.claude/rules/frontend.md` warns about — Tailwind drops an
        unknown colour class in silence — and this suite was written to
        catch it. At 0.2 it did not; at 0.03 it does, verified by
        breaking the class and watching it fail.
      */
      threshold: 0.03,
    },
  },

  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
})
