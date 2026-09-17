import AxeBuilder from '@axe-core/playwright'
import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The chart screen, against the real engine.
 *
 * `ChartSVG` existed from Phase 3 PR 2 with thirty passing unit tests
 * and no route — nothing a user could open. This covers the two gate
 * items that rested on it: that a real chart renders in both styles, and
 * that the style switcher persists to preferences rather than being a
 * view toggle that forgets.
 */
test.use({ ...devices['Pixel 7'], viewport: { width: 360, height: 780 } })
test.describe.configure({ mode: 'serial' })

let shared: Page
let context: BrowserContext

test.beforeAll(async ({ browser }) => {
  context = await browser.newContext({
    ...devices['Pixel 7'],
    viewport: { width: 360, height: 780 },
  })
  shared = await context.newPage()
  await signUp(shared)
  await addBirthProfile(shared)
})

test.afterAll(async () => {
  await context.close()
})

async function signUp(page: Page): Promise<void> {
  const phone = uniquePhone('906')

  await page.goto('/auth')
  await page.getByRole('button', { name: /use phone instead/i }).click()

  const otp = watchOTP(phone)
  await page.getByLabel(/phone number/i).fill(phone)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Priya')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)
}

async function addBirthProfile(page: Page): Promise<void> {
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

/** The chart's own SVG, not the switcher dots. */
const CHART_SVG = 'main svg'

test.describe('the chart screen', () => {
  test('draws a real chart with all nine planets', async () => {
    const page = shared
    await page.goto('/kundli/chart')

    await expect(page.locator(CHART_SVG)).toBeVisible({ timeout: 30_000 })

    /*
      The visually-hidden table is the chart's own data, so counting its
      rows counts what was drawn. Nine grahas plus a header, and the
      ascendant row when there is a birth time.
    */
    const table = page.getByRole('table')
    await expect(table).toBeAttached()
    for (const planet of ['Sun', 'Moon', 'Mars', 'Mercury', 'Jupiter', 'Venus', 'Saturn', 'Rahu', 'Ketu']) {
      await expect(table.getByRole('row', { name: new RegExp(planet) }), planet).toBeAttached()
    }
  })

  /**
   * The two layouts are structurally different, not restyled.
   *
   * North Indian houses are triangles and South Indian cells are
   * rectangles, so the polygon count differs. Asserting the SVG merely
   * "changed" would pass on a re-render that changed nothing; asserting
   * the shape count is what distinguishes the layouts.
   */
  test('switching style redraws the diagram in the other layout', async () => {
    const page = shared
    await page.goto('/kundli/chart')
    await expect(page.locator(CHART_SVG)).toBeVisible({ timeout: 30_000 })

    await page.locator('label:has(input[name="chart-style"][value="north"])').click()
    await expect(page.getByRole('radio', { name: /north/i })).toBeChecked()
    const northShapes = await page.locator(`${CHART_SVG} polygon`).count()

    await page.locator('label:has(input[name="chart-style"][value="south"])').click()
    await expect(page.getByRole('radio', { name: /south/i })).toBeChecked()
    const southShapes = await page.locator(`${CHART_SVG} polygon`).count()

    expect(northShapes, 'the North Indian chart drew no regions').toBeGreaterThan(0)
    expect(southShapes, 'the South Indian chart drew no regions').toBeGreaterThan(0)
    expect(
      southShapes,
      'both styles drew the same number of regions — the diagram did not change layout',
    ).not.toBe(northShapes)
  })

  /**
   * Gate item: "switcher persists to preferences".
   *
   * A view toggle that forgets on reload would satisfy the test above.
   * This checks the write landed on the ACCOUNT — reloading is not
   * enough on its own, because a value cached in localStorage would
   * survive that too, so it reads the preference back from the API.
   */
  test('persists the chosen style to the account', async () => {
    const page = shared
    await page.goto('/kundli/chart')
    await expect(page.locator(CHART_SVG)).toBeVisible({ timeout: 30_000 })

    await page.locator('label:has(input[name="chart-style"][value="south"])').click()
    await expect(page.getByRole('radio', { name: /south/i })).toBeChecked()

    /*
      Read it back through the settings screen rather than a hand-rolled
      fetch.

      The first version called the preferences endpoint from
      `page.evaluate`, which fails: `authed()` sends a Bearer token held
      in memory, not a cookie, so a raw fetch is unauthenticated and the
      poll never resolves. Driving the screen that already displays the
      preference tests the same thing without reimplementing auth in a
      test — and it fails if the write never reached the account.
    */
    await page.goto('/settings/preferences')
    await expect(page.getByRole('radio', { name: /south/i })).toBeChecked({ timeout: 30_000 })

    // And a fresh load comes back in that style, from the server.
    await page.goto('/kundli/chart')
    await expect(page.getByRole('radio', { name: /south/i })).toBeChecked({ timeout: 30_000 })
  })

  test('does not scroll sideways at 360px', async () => {
    const page = shared
    await page.goto('/kundli/chart')
    await expect(page.locator(CHART_SVG)).toBeVisible({ timeout: 30_000 })

    const device = page.viewportSize()!
    const content = await page.evaluate(() => document.documentElement.scrollWidth)
    expect(
      content,
      `the chart screen is ${content}px on a ${device.width}px device`,
    ).toBeLessThanOrEqual(device.width + 1)
  })

  test('reports no accessibility violations', async () => {
    const page = shared
    await page.goto('/kundli/chart')
    await expect(page.locator(CHART_SVG)).toBeVisible({ timeout: 30_000 })

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze()

    const summary = results.violations.map((v) => ({
      id: v.id,
      impact: v.impact,
      nodes: v.nodes.map((n) => n.target.join(' ')),
    }))
    expect(summary, JSON.stringify(summary, null, 2)).toEqual([])
  })
})
