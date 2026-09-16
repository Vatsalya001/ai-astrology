import AxeBuilder from '@axe-core/playwright'
import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The dashboard, against the real stack.
 *
 * Its whole job is assembling four independent sources — the user, the
 * profile list, the current dasha periods and today's transits — and
 * the interesting failures are about what happens when one of them is
 * missing. Those are the ones a unit test cannot reach, because the
 * missing source has to actually be missing.
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
})

test.afterAll(async () => {
  await context.close()
})

async function signUp(page: Page): Promise<void> {
  const phone = uniquePhone('904')

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

test.describe('with no birth profile yet', () => {
  /**
   * The state a brand-new account is actually in.
   *
   * `currentDashas` and `transits` both need a profile, so both reject,
   * and the page was written with `Promise.all` first — which turned the
   * ENTIRE dashboard into an error page for every new user on their very
   * first visit. `allSettled` is why the greeting still renders.
   */
  test('still greets the user and shows the screen', async () => {
    const page = shared
    await page.goto('/home')

    await expect(page.getByRole('heading', { name: /Priya/ })).toBeVisible({ timeout: 30_000 })
    await expect(page.getByRole('heading', { name: /something went wrong/i })).toHaveCount(0)
    await expect(page.getByText(/something went wrong/i)).toHaveCount(0)
  })

  test('greets by name, with no stray comma', async () => {
    const page = shared
    await page.goto('/home')

    const heading = page.getByRole('heading', { level: 1 })
    await expect(heading).toBeVisible({ timeout: 30_000 })

    const text = ((await heading.textContent()) ?? '').trim()
    expect(text, `greeting was "${text}"`).toMatch(
      /^(Good morning|Good afternoon|Good evening|Hello), Priya$/,
    )
  })

  /**
   * The AI box is present and genuinely disabled.
   *
   * `features.ai_chat` is false until Phase 5. "Present but disabled" is
   * the spec's ask, and the failure mode worth guarding is a box that
   * merely LOOKS inert while still accepting a question that goes
   * nowhere.
   */
  test('shows the AI box, disabled, with a reason', async () => {
    const page = shared
    await page.goto('/home')

    const ask = page.getByRole('textbox', { name: /ask your ai astrologer/i })
    await expect(ask).toBeVisible({ timeout: 30_000 })
    await expect(ask).toBeDisabled()

    for (const topic of ['Career', 'Love', 'Money', 'Marriage']) {
      await expect(page.getByRole('button', { name: topic })).toBeDisabled()
    }

    await expect(page.getByText(/not available yet/i)).toBeVisible()
  })
})

test.describe('with a chart', () => {
  test.beforeAll(async () => {
    await addBirthProfile(shared)
  })

  test('shows today from the transiting Moon', async () => {
    const page = shared
    await page.goto('/home')
    await expect(page.getByRole('heading', { name: /today/i })).toBeVisible({ timeout: 30_000 })

    const card = page.locator('section', { has: page.getByRole('heading', { name: /^Today$/i }) })
    const text = (await card.textContent()) ?? ''

    // A real sign from the engine, and a written line about it.
    expect(text).toMatch(
      /Moon in (Aries|Taurus|Gemini|Cancer|Leo|Virgo|Libra|Scorpio|Sagittarius|Capricorn|Aquarius|Pisces)/,
    )
    expect(text, 'the corpus has no line for the sign the engine reported').not.toMatch(
      /no note written/i,
    )

    /*
      No nakshatra and no tithi. The engine reports neither, and
      deriving them here would be the frontend computing astrology —
      asserted so that adding one is a failing test rather than a
      plausible-looking improvement.
    */
    expect(text).not.toMatch(/nakshatra|tithi/i)
  })

  test('shows the current mahadasha with a real progress value', async () => {
    const page = shared
    await page.goto('/home')

    const bar = page.getByRole('progressbar')
    await expect(bar).toBeVisible({ timeout: 30_000 })

    const value = Number(await bar.getAttribute('aria-valuenow'))
    expect(value).toBeGreaterThanOrEqual(0)
    expect(value).toBeLessThanOrEqual(100)

    // Written out beside the bar, not only encoded in its width.
    await expect(page.getByText(new RegExp(`${value}% elapsed`))).toBeVisible()
  })

  test('shows the Sade Sati state', async () => {
    const page = shared
    await page.goto('/home')
    await expect(page.getByText(/sade sati/i).first()).toBeVisible({ timeout: 30_000 })
    await expect(page.getByText(/Saturn is in/i)).toBeVisible()
  })

  test('links through to the full Kundli', async () => {
    const page = shared
    await page.goto('/home')

    const link = page.getByRole('link', { name: /view my full kundli/i })
    await expect(link).toBeVisible({ timeout: 30_000 })
    await link.click()
    await expect(page).toHaveURL(/\/kundli\/planets/)
  })

  test('does not scroll sideways at 360px', async () => {
    const page = shared
    await page.goto('/home')
    await expect(page.getByRole('heading', { name: /today/i })).toBeVisible({ timeout: 30_000 })

    const device = page.viewportSize()!
    const content = await page.evaluate(() => document.documentElement.scrollWidth)
    expect(
      content,
      `the dashboard is ${content}px on a ${device.width}px device`,
    ).toBeLessThanOrEqual(device.width + 1)
  })

  test('reports no accessibility violations', async () => {
    const page = shared
    await page.goto('/home')
    await expect(page.getByRole('heading', { name: /today/i })).toBeVisible({ timeout: 30_000 })

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
