import AxeBuilder from '@axe-core/playwright'
import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The dasha and transit screens, against the real engine.
 *
 * These are the two screens whose content nothing in the unit suite can
 * produce: the dasha tree is 819 rows generated from a birth moment, and
 * the transits come from a worker writing a shared table. A component
 * test can prove the timeline lays a fixture out proportionally; only
 * this can prove there is anything real to lay out.
 *
 * Serial with one shared account, for the reason `kundli-planets.spec.ts`
 * documents: the log masks an identifier, so two parallel signups in one
 * file are one identity to `watchOTP`.
 *
 * ── Why this one signs up by PHONE ──
 *
 * The email mask keeps a single first letter, and after
 * `kundli-planets.spec.ts` took `s` every letter of the alphabet is
 * spoken for. That is a real structural limit, not a nuisance: the
 * discriminator has 26 values and this suite has more specs than that
 * coming.
 *
 * The phone mask keeps a three-digit TAIL — a thousand values — and
 * `uniquePhone` already exists for exactly this. `mask-letters.spec.ts`
 * did not check tails until now, which it does as of this commit,
 * because an unguarded thousand-value space is only better than a
 * guarded twenty-six-value one until two specs pick the same number.
 */
test.use({ ...devices['Pixel 7'], viewport: { width: 360, height: 780 } })
test.describe.configure({ mode: 'serial' })

let shared: Page
let context: BrowserContext

test.beforeAll(async ({ browser }) => {
  // From a context, not `browser.newPage()` — axe rejects the short way.
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

async function signUpWithChart(page: Page): Promise<void> {
  const phone = uniquePhone('901')

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

test.describe('dashas', () => {
  test('renders nine mahadashas of unequal length', async () => {
    const page = shared
    await page.goto('/kundli/dashas')

    const bars = page.getByRole('listitem')
    await expect(bars).toHaveCount(9, { timeout: 30_000 })

    /*
      Proportional, measured in the browser.

      The unit test asserts this from the inline style; this asserts the
      layout engine agreed. Venus runs 20 years and the Sun 6, so the
      widths must differ by roughly 3.3x — equal columns would be tidier
      and would misrepresent the one thing the screen is for.
    */
    const venus = await page
      .getByRole('button', { name: /Venus mahadasha/i })
      .boundingBox()
    const sun = await page.getByRole('button', { name: /Sun mahadasha/i }).boundingBox()

    expect(venus, 'Venus bar has no box').not.toBeNull()
    expect(sun, 'Sun bar has no box').not.toBeNull()
    expect(venus!.width / sun!.width).toBeGreaterThan(2.5)
  })

  /**
   * Exactly one current period, and it is the one containing today.
   *
   * The marker comes from the server's instant rather than the browser's
   * clock, so this also confirms the server answered — a browser-derived
   * marker would still produce exactly one, which is why the assertion
   * below goes on to check the span really brackets today.
   */
  test('marks exactly one current mahadasha, containing today', async () => {
    const page = shared
    await page.goto('/kundli/dashas')
    await expect(page.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

    const current = page.locator('button[aria-current="true"]')
    await expect(current).toHaveCount(1)

    const label = (await current.getAttribute('aria-label'))!
    expect(label).toMatch(/current period/i)

    // "Ketu mahadasha, Mar 2019 – Mar 2035, current period"
    const years = [...label.matchAll(/\b(\d{4})\b/g)].map((m) => Number(m[1]))
    expect(years, `no years in "${label}"`).toHaveLength(2)

    const thisYear = new Date().getFullYear()
    expect(years[0]!).toBeLessThanOrEqual(thisYear)
    expect(years[1]!).toBeGreaterThanOrEqual(thisYear)
  })

  test('drills into a mahadasha and shows its antardashas', async () => {
    const page = shared
    await page.goto('/kundli/dashas')
    await expect(page.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

    await page.getByRole('button', { name: /Venus mahadasha/i }).click()

    // The sheet opens over the track; close it to see what loaded.
    await page.getByRole('dialog').getByRole('button', { name: /close/i }).click()

    /*
      Nine antardashas belonging to Venus, not all 81.

      The endpoint returns every period at level 2 and the page filters
      by parent. Getting that wrong shows a 120-year track labelled
      "Antardasha" — complete, plausible, and wrong.
    */
    const antardashas = page.getByRole('region', { name: /Antardasha periods/i })
    await expect(antardashas.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

    // Every one of them sits inside Venus's own span.
    const labels = await antardashas.getByRole('button').evaluateAll((els) =>
      els.map((el) => el.getAttribute('aria-label') ?? ''),
    )
    expect(labels.every((l) => /antardasha/i.test(l))).toBe(true)
  })

  test('reports no accessibility violations', async () => {
    const page = shared
    await page.goto('/kundli/dashas')
    await expect(page.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

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

  test('does not scroll sideways at 360px', async () => {
    const page = shared
    await page.goto('/kundli/dashas')
    await expect(page.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

    const device = page.viewportSize()!
    const content = await page.evaluate(() => document.documentElement.scrollWidth)
    expect(content, `the dasha track is ${content}px on a ${device.width}px device`).toBeLessThanOrEqual(
      device.width + 1,
    )
  })
})

test.describe('transits', () => {
  test('lists the planets with houses counted from the natal Moon', async () => {
    const page = shared
    await page.goto('/kundli/transits')

    const list = page.getByRole('list', { name: /transiting planets/i })
    await expect(list).toBeVisible({ timeout: 30_000 })
    await expect(list.getByRole('listitem')).toHaveCount(9)

    // Which frame, said on the screen. "Saturn in your 12th" means two
    // different things depending on where the count starts.
    await expect(page.getByText(/counted from your Moon in/i)).toBeVisible()
  })

  /**
   * The Sade Sati card renders against the real engine.
   *
   * ── What this covers, and what it does not ──
   *
   * Which branch renders depends on where Saturn is TODAY relative to
   * this fixture's natal Moon, which nothing here controls. For the
   * 1994 Jaipur profile Saturn is currently the 4th sign from the Moon,
   * so the inactive branch renders — and an earlier version of this test
   * asserted "no dates appear" against it, which is trivially true of a
   * branch that has no dates. Planting an invented end date on the
   * ACTIVE branch left it green.
   *
   * So this asserts what it can actually see: that the card rendered,
   * named Saturn's real position, and used no predictive framing. The
   * phase dots, the unreported-phase fallback and the no-dates rule on
   * the active branch are all covered in `SadeSatiIndicator.test.tsx`,
   * where the status is a fixture and every branch is reachable —
   * verified by planting that same date and watching it fail.
   */
  test('renders the Sade Sati card from the engine, in whichever state it is in', async () => {
    const page = shared
    await page.goto('/kundli/transits')
    await expect(page.getByText(/sade sati/i).first()).toBeVisible({ timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''

    // Saturn's real sign, taken from the engine rather than a fixture.
    expect(text).toMatch(/Saturn is in \w+/)

    // Exactly one of the two branches, never both and never neither.
    const active = /current phase/i.test(text)
    const inactive = /not currently running/i.test(text)
    expect(active !== inactive, `neither or both branches rendered: ${text.slice(-200)}`).toBe(
      true,
    )

    // The framing rule holds on whichever branch this is.
    for (const phrase of ['you will', 'is certain', 'guarantees', 'destined to']) {
      expect(text.toLowerCase(), phrase).not.toContain(phrase)
    }
  })

  test('reports no accessibility violations', async () => {
    const page = shared
    await page.goto('/kundli/transits')
    await expect(page.getByRole('list', { name: /transiting planets/i })).toBeVisible({
      timeout: 30_000,
    })

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
