import AxeBuilder from '@axe-core/playwright'
import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * The planets and houses screen, at the width most of its users have.
 *
 * Task 3.7's acceptance is "responsive; no horizontal scroll at 360px",
 * and 360 is the narrowest width this product has to work at — a large
 * share of Indian Android devices report exactly it. A six-column table
 * is the densest thing in the app, so if anything overflows, this does.
 *
 * `test.use` at an explicit 360px rather than a device preset: the
 * acceptance names a number, and Pixel 7's 412 would pass a layout that
 * fails the requirement by 52 pixels.
 */
test.use({ ...devices['Pixel 7'], viewport: { width: 360, height: 780 } })

/**
 * Serial, with one account shared by all three tests.
 *
 * Not a style choice. `uniqueEmail('s')` produces a distinct address per
 * call, but the API log masks it to `s***@example.com` — only the first
 * letter survives — so three signups running in parallel are one
 * identity as far as `watchOTP` is concerned, and they read each other's
 * codes. The failure reads as "wrong code", which looks like broken
 * authentication rather than a test collision.
 *
 * `mask-letters.spec.ts` guards this ACROSS specs; within one spec it is
 * serial mode plus a shared page that does it. Signing up once is also
 * three fewer birth-chart computations per run.
 */
test.describe.configure({ mode: 'serial' })

let shared: Page
let context: BrowserContext

test.beforeAll(async ({ browser }) => {
  // `newContext().newPage()`, not `browser.newPage()`. Axe rejects a
  // page created the short way — "Please use browser.newContext()" —
  // and the accessibility test below is the whole reason this screen
  // gets checked at all.
  context = await browser.newContext({
    ...devices['Pixel 7'],
    viewport: { width: 360, height: 780 },
  })
  shared = await context.newPage()
  await signUpWithChart(shared)
})

test.afterAll(async () => {
  await context.close()
})

/** Signs up and completes the birth flow, so there is a chart to render. */
async function signUpWithChart(page: Page): Promise<void> {
  const email = uniqueEmail('s')

  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Priya')
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
  const first = page.getByRole('option').first()
  await expect(first).toBeVisible()
  await first.click()

  await page.getByRole('button', { name: /see my kundli/i }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })
}

test.describe('planets and houses at 360px', () => {
  test('renders the chart and does not scroll sideways', async () => {
    const page = shared
    await page.setViewportSize({ width: 360, height: 780 })
    await page.goto('/kundli/planets')

    /*
      Assert the table is POPULATED before measuring the width.

      An empty state cannot overflow, so a width check that runs against
      "add your birth details" passes for the wrong reason and keeps
      passing after the table is broken. Nine grahas, and Saturn by name.
    */
    const rows = page.getByRole('row')
    await expect(rows).toHaveCount(10, { timeout: 30_000 }) // 9 planets + header
    await expect(page.getByRole('button', { name: /Saturn in /i })).toBeVisible()

    // The houses too, since they render from the same payload.
    await expect(page.getByRole('listitem')).toHaveCount(12)

    /*
      The reference width is the harness's viewport, NOT the page's
      `window.innerWidth`. Under mobile emulation Chrome expands the
      layout viewport to fit overflowing content, so `innerWidth` grows
      in lockstep with `scrollWidth` and comparing them can never fail.
      This is the same trap `mobile.spec.ts` documents.
    */
    const device = page.viewportSize()
    expect(device, 'no viewport — emulation is not applied').not.toBeNull()

    const content = await page.evaluate(() => document.documentElement.scrollWidth)
    expect(
      content,
      `the planets table is ${content}px wide on a ${device!.width}px device`,
    ).toBeLessThanOrEqual(device!.width + 1)
  })

  /**
   * The restack is CSS, so jsdom cannot see it — the unit tests assert
   * the roles and the data-labels are present, and this asserts they do
   * the job.
   *
   * Measured as the geometry of the cells themselves. The first version
   * checked that the column header was off-screen, via its bounding box,
   * and failed against a correct layout: `sr-only` on the `<thead>`
   * hides it visually but its `<th>` children still lay out in an
   * anonymous table and report a real width. It was testing the wrong
   * element for the wrong property.
   *
   * Stacked means the cells in one row have different y positions. Six
   * columns means they share one. That is the property, stated directly,
   * and the desktop half is what stops it passing on a page that failed
   * to render.
   */
  test('stacks each row into a card at 360px, and does not at desktop width', async () => {
    const page = shared
    // Restored explicitly: this test widens the viewport at the end, and
    // serial mode means the next test inherits whatever it leaves.
    await page.setViewportSize({ width: 360, height: 780 })
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const firstRow = page.getByRole('row').nth(1)
    const tops = async () =>
      Promise.all(
        ['Planet', 'Sign', 'Status'].map(async (label) => {
          const box = await firstRow.locator(`[data-label="${label}"]`).boundingBox()
          expect(box, `${label} cell has no box`).not.toBeNull()
          return box!.y
        }),
      )

    const stacked = await tops()
    expect(new Set(stacked).size, `cells share a row at 360px: ${stacked.join(', ')}`).toBe(3)

    // And the same cells sit on one line once there is room for them.
    await page.setViewportSize({ width: 1280, height: 800 })
    const sideBySide = await tops()
    expect(
      new Set(sideBySide).size,
      `cells are still stacked at 1280px: ${sideBySide.join(', ')}`,
    ).toBe(1)
  })

  test('switching to the dasamsa loads a different chart', async () => {
    const page = shared
    await page.setViewportSize({ width: 360, height: 780 })
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const saturnInD1 = await page
      .getByRole('button', { name: /Saturn in /i })
      .getAttribute('aria-label')

    /*
      The label, not the input. The radios are `sr-only` — one pixel,
      clipped — which is how the segmented control gets the browser's
      keyboard behaviour without showing a bare radio button. Playwright
      refuses to click an element outside the viewport, and `force: true`
      would paper over a genuinely unclickable control. Clicking the
      label is what a user does and is what must work.
    */
    await page.locator('label:has(input[value="D10"])').click()
    await expect(page.getByRole('radio', { name: /dasamsa/i })).toBeChecked()
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const saturnInD10 = await page
      .getByRole('button', { name: /Saturn in /i })
      .getAttribute('aria-label')

    /*
      The D10 is a different chart, not a relabelled D1 — which is
      exactly the bug Phase 2's gate found, where the chart type never
      reached astro-service and both rows held the identical payload.
      Nothing about that looked wrong on screen either.

      Saturn moves between the rasi and the dasamsa for this birth data;
      if a future fixture makes them coincide, this needs a different
      planet rather than a weaker assertion.
    */
    expect(saturnInD10).not.toBe(saturnInD1)
    await expect(page.getByText(/career, profession and public standing/i)).toBeVisible()
  })

  /**
   * The densest screen in the app, checked the way the others are.
   *
   * It is also the one most likely to fail: a table whose semantics are
   * restated by hand, a restack that changes `display` on `<tr>` and
   * `<td>`, two dialogs, and a radio group with visually-hidden inputs.
   * Every one of those is a way to produce something that looks right
   * and announces nothing.
   */
  test('reports no accessibility violations', async () => {
    const page = shared
    await page.setViewportSize({ width: 360, height: 780 })
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

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

  /**
   * The restacked table is still a table, in the tree that matters.
   *
   * Not the DOM, and not axe: axe evaluates implicit ARIA against the
   * markup, so it passes whether or not the explicit roles are there —
   * verified by removing `role="row"` and watching it stay green. It
   * cannot see this property at all.
   *
   * `ariaSnapshot` reads the computed accessibility tree, which is where
   * the question actually lives: at 360px the rows are `display: block`
   * and the cells `display: flex`, and what must survive that is the
   * table structure a screen reader navigates by. Asserting it here is
   * the only check in the suite that would notice if it did not.
   */
  test('is still a table in the accessibility tree once restacked', async () => {
    const page = shared
    await page.setViewportSize({ width: 360, height: 780 })
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const tree = await page.locator('table').ariaSnapshot()

    expect(tree, 'the restacked table no longer reports as a table').toContain('table')
    expect(tree, 'rows have been flattened out of the tree').toContain('row')
    expect(tree, 'cells have been flattened out of the tree').toContain('cell')

    // And the columns are still associated, which is what makes a cell
    // in a restacked card mean anything.
    await expect(page.getByRole('columnheader')).toHaveCount(6)
  })
})
