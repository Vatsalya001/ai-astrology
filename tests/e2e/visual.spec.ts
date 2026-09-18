import { expect, test, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * The visual regression suite.
 *
 * Gate item: "Visual regression suite green at 3 viewports × 2 themes."
 *
 * ── Three viewports, and ONE theme ──
 *
 * The three viewports are here: 360px (the phone this product is
 * designed for first), 768px, and 1280px.
 *
 * The second theme is not, because it does not exist.
 * `.claude/rules/frontend.md` is explicit: "the app is dark-only, so a
 * light mode nobody can reach is dead weight that still has to be kept
 * correct." Screenshotting a theme no user can select would lock in the
 * appearance of a code path that is never executed — the opposite of
 * what a regression suite is for. Recorded in PROJECT_STATUS.md as a
 * spec item met in part, deliberately, rather than quietly counted as
 * done.
 *
 * ── Why this exists at all ──
 *
 * The chart is geometry. `ChartSVG.fixtures.test.tsx` proves the right
 * planets land in the right houses for 30 fixtures, and geometry unit
 * tests prove the polygons are the shapes they should be. Neither
 * notices if a stylesheet change makes the glyphs invisible, collapses
 * the grid to zero height, or leaves a house label outside its cell —
 * because both assert on data, and none of those defects change any
 * data.
 *
 * That is exactly the class of bug this project has already shipped
 * once: a Tailwind class naming a colour the config does not define is
 * dropped in silence, and the element renders with no background.
 *
 * ── Why masks rather than a frozen clock ──
 *
 * Several values on these screens are genuinely time-dependent: which
 * dasha is running, where the "you are here" marker sits, today's
 * transits. Those come from the SERVER, so freezing the browser's clock
 * would not change them — it would only desynchronise the two.
 *
 * So the volatile regions are masked: Playwright paints over them, and
 * the screenshot compares the layout, the geometry and the colours,
 * which are the things a stylesheet regression actually breaks.
 */

const VIEWPORTS = [
  { name: 'phone', width: 360, height: 780 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1280, height: 900 },
] as const

test.describe.configure({ mode: 'serial' })

/**
 * One account with one birth profile, reused across every shot.
 *
 * A fresh signup per screenshot would be slower and, worse, would give
 * each shot a different profile id — and the profile switcher renders
 * a label derived from it. Same account, same chart, every time.
 */
async function signUpWithAProfile(page: Page): Promise<void> {
  const email = uniqueEmail('v')
  const otp = watchOTP(email)

  await page.goto('/auth')
  await page.getByLabel(/email/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Vimala')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)

  await page.goto('/onboarding/birth')
  await page.getByLabel(/^day$/i).fill('17')
  await page.getByLabel(/^month$/i).fill('8')
  await page.getByLabel(/^year$/i).fill('1994')
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/^hour$/i).fill('14')
  await page.getByLabel(/^minute$/i).fill('35')
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/birth place/i).fill('jaip')
  await expect(page.getByRole('option').first()).toBeVisible()
  await page.getByRole('option').first().click()

  await page.getByRole('button', { name: /see my kundli/i }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })
}

/**
 * Waits until the page is safe to photograph.
 *
 * Fonts first: the chart's glyphs and every degree are text, and a shot
 * taken while a webfont is still in flight captures fallback metrics —
 * which differ from the real ones by enough to fail the comparison on
 * the next run, when the font is cached.
 *
 * Then animations off. `prefers-reduced-motion` is honoured by this app
 * (an accessibility requirement in its own right), so setting it also
 * stops the starfield twinkling mid-capture.
 */
async function settle(page: Page): Promise<void> {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.waitForLoadState('networkidle')
  await page.evaluate(() => document.fonts.ready)
  // One frame, so the browser has painted with those fonts rather than
  // merely having them available.
  await page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
  )
}

/**
 * Regions whose CONTENT changes with the clock.
 *
 * Masked, not excluded: the element's box still has to be the right size
 * in the right place, which is what a layout regression breaks. Only the
 * glyphs inside it are painted over.
 */
function volatile(page: Page) {
  return [
    // "Running now", elapsed percentages, the dasha marker.
    page.locator('[aria-current="true"]'),
    page.locator('[role="progressbar"]'),
    // Today's sky, and the Sade Sati phase.
    page.locator('[data-volatile]'),
  ]
}

for (const viewport of VIEWPORTS) {
  test.describe(`${viewport.name} (${viewport.width}px)`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } })

    let page: Page

    test.beforeAll(async ({ browser }) => {
      page = await browser.newPage({
        viewport: { width: viewport.width, height: viewport.height },
      })
      await signUpWithAProfile(page)
    })

    test.afterAll(async () => {
      await page.close()
    })

    /*
      The chart itself, shot on its own rather than as part of the page.

      A full-page shot of /kundli/chart would fail on any change to the
      header, the switchers or the footer — none of which is what this
      test is about. Scoping it to the SVG means a failure here means the
      chart changed, which is a diagnosis rather than a prompt to go
      looking.
    */
    test('the rasi chart', async () => {
      await page.goto('/kundli/chart')
      await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })
      await settle(page)

      await expect(page.locator('svg[role="group"]')).toHaveScreenshot(
        `chart-rasi-${viewport.name}.png`,
      )
    })

    test('the chart screen', async () => {
      await page.goto('/kundli/chart')
      await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })
      await settle(page)

      await expect(page).toHaveScreenshot(`screen-chart-${viewport.name}.png`, {
        fullPage: true,
        mask: volatile(page),
      })
    })

    test('the planets screen', async () => {
      await page.goto('/kundli/planets')
      await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })
      await settle(page)

      await expect(page).toHaveScreenshot(`screen-planets-${viewport.name}.png`, {
        fullPage: true,
        mask: volatile(page),
      })
    })

    test('the dasha screen', async () => {
      await page.goto('/kundli/dashas')
      await expect(page.getByRole('listitem').first()).toBeVisible({ timeout: 30_000 })
      await settle(page)

      await expect(page).toHaveScreenshot(`screen-dashas-${viewport.name}.png`, {
        fullPage: true,
        mask: volatile(page),
      })
    })

    test('the yogas screen', async () => {
      await page.goto('/kundli/yogas')
      await expect(page.getByRole('button').first()).toBeVisible({ timeout: 30_000 })
      await settle(page)

      await expect(page).toHaveScreenshot(`screen-yogas-${viewport.name}.png`, {
        fullPage: true,
        mask: volatile(page),
      })
    })
  })
}
