import AxeBuilder from '@axe-core/playwright'
import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * Yogas from the real engine, and switching between two charts.
 *
 * Two profiles, because task 3.14's acceptance is "switching re-renders
 * everything correctly" and one profile cannot demonstrate that. The
 * second is a different birth moment on purpose — a switcher that
 * re-renders with identical content proves nothing.
 *
 * Phone signup with a distinct three-digit tail: the email mask keeps
 * one letter and the alphabet is spoken for. See `kundli-dashas.spec.ts`.
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
  await addBirthProfile(shared, { day: '17', month: '8', year: '1994', hour: '14', minute: '35' })
  await addBirthProfile(shared, { day: '3', month: '2', year: '1988', hour: '6', minute: '10' })
})

test.afterAll(async () => {
  await context.close()
})

async function signUp(page: Page): Promise<void> {
  const phone = uniquePhone('902')

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

async function addBirthProfile(
  page: Page,
  d: { day: string; month: string; year: string; hour: string; minute: string },
): Promise<void> {
  await page.goto('/onboarding/birth')
  await page.getByLabel(/^day$/i).fill(d.day)
  await page.getByLabel(/^month$/i).fill(d.month)
  await page.getByLabel(/^year$/i).fill(d.year)
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/^hour$/i).fill(d.hour)
  await page.getByLabel(/^minute$/i).fill(d.minute)
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/birth place/i).fill('jaip')
  const first = page.getByRole('option').first()
  await expect(first).toBeVisible()
  await first.click()

  await page.getByRole('button', { name: /see my kundli/i }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })
}

test.describe('yogas', () => {
  /**
   * Whatever the engine found, described.
   *
   * Which combinations this chart has is not fixed by anything here, so
   * the assertion is about the CONTRACT rather than about a particular
   * yoga: every card that renders carries a written description, and
   * none carries the "no description written" fallback. That fallback
   * appearing means the engine emitted a name the corpus does not cover
   * — the exact drift `yogas.test.ts` guards against in one direction,
   * confirmed here in the other, against real output.
   */
  test('every yoga the engine found has a written description', async () => {
    const page = shared
    await page.goto('/kundli/yogas')

    // Either cards or the explicit "found none" panel — never a blank.
    await expect(
      page.getByRole('button').or(page.getByText(/found none of them/i)).first(),
    ).toBeVisible({ timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''
    expect(
      text,
      'the engine emitted a yoga name the corpus has no entry for',
    ).not.toMatch(/no description written/i)

    const cards = page.locator('main button')
    const count = await cards.count()

    if (count === 0) {
      expect(text).toMatch(/found none of them/i)
      return
    }

    // Every card says Strong or Moderate, and none of them says a number.
    for (let i = 0; i < count; i++) {
      const card = (await cards.nth(i).textContent()) ?? ''
      expect(card, `card ${i}`).toMatch(/Strong|Moderate/)
    }
  })

  /**
   * Strength never becomes a number, on real output.
   *
   * astro-service is explicit that a score "would imply a precision the
   * tradition does not have". The unit test asserts this on a fixture;
   * this asserts it on whatever the engine actually returned, where a
   * house number or a degree could have leaked into the card.
   */
  test('shows no score, percentage or star rating', async () => {
    const page = shared
    await page.goto('/kundli/yogas')
    await expect(
      page.getByRole('button').or(page.getByText(/found none of them/i)).first(),
    ).toBeVisible({ timeout: 30_000 })

    const cards = page.locator('main button')
    for (let i = 0; i < (await cards.count()); i++) {
      const card = (await cards.nth(i).textContent()) ?? ''
      expect(card, `card ${i}: ${card}`).not.toMatch(/\d+\s*%|★|\d+\s*\/\s*(5|10|100)/)
    }
  })

  test('reports no accessibility violations', async () => {
    const page = shared
    await page.goto('/kundli/yogas')
    await expect(
      page.getByRole('button').or(page.getByText(/found none of them/i)).first(),
    ).toBeVisible({ timeout: 30_000 })

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

test.describe('the profile switcher', () => {
  test('appears once there are two charts, and lists both', async () => {
    const page = shared
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const switcher = page.getByRole('combobox', { name: /chart/i })
    await expect(switcher).toBeVisible()
    await expect(switcher.locator('option')).toHaveCount(2)
  })

  /**
   * Task 3.14's acceptance: switching re-renders everything.
   *
   * Two different birth moments, so the charts genuinely differ. A
   * switcher that changed its own value and left the table alone would
   * pass every other test in this file — which is why this compares the
   * rendered placement before and after rather than the select's value.
   */
  test('switching re-renders the planetary table with the other chart', async () => {
    const page = shared
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const sun = page.getByRole('button', { name: /^Sun in /i })
    const before = await sun.getAttribute('aria-label')

    const switcher = page.getByRole('combobox', { name: /chart/i })
    const options = await switcher.locator('option').evaluateAll((els) =>
      els.map((el) => (el as HTMLOptionElement).value),
    )
    // Resolved before the callback: `await` inside a non-async `.find`
    // is a syntax error, and a spec file that fails to parse is reported
    // by Playwright as "No tests found" rather than as an error — the
    // whole file silently absent from the run.
    const current = await switcher.inputValue()
    const other = options.find((v) => v !== current)
    expect(other, 'only one option — the second profile did not get created').toBeDefined()
    await switcher.selectOption(other!)

    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })
    await expect
      .poll(async () => page.getByRole('button', { name: /^Sun in /i }).getAttribute('aria-label'))
      .not.toBe(before)
  })

  /**
   * The selection survives navigation between screens.
   *
   * It lives in localStorage rather than in a URL parameter — a profile
   * id is a durable handle to a record holding birth details, and the
   * security rules keep those out of query strings. The cost of that
   * choice is that persistence is now something to verify rather than
   * something the URL gives for free.
   */
  test('keeps the chosen chart when moving between screens', async () => {
    const page = shared
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const chosen = await page.getByRole('combobox', { name: /chart/i }).inputValue()

    await page.goto('/kundli/yogas')
    await expect(page.getByRole('combobox', { name: /chart/i })).toHaveValue(chosen)

    await page.goto('/kundli/dashas')
    await expect(page.getByRole('combobox', { name: /chart/i })).toHaveValue(chosen)
  })

  /**
   * A stored id naming a profile that no longer exists.
   *
   * Editing a birth profile creates a NEW version with a new id rather
   * than mutating the old one, so this is not hypothetical — it happens
   * the first time anyone corrects their birth time. Requesting the
   * stale id gives a 404 that is indistinguishable from "not yours", so
   * without the fallback the screen shows an error state forever, on
   * every visit, with nothing to indicate the fix.
   */
  test('recovers from a stored id that matches no profile', async () => {
    const page = shared
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    await page.evaluate(() =>
      window.localStorage.setItem('ayana.birthProfileId', '00000000-0000-0000-0000-000000000000'),
    )
    await page.reload()

    // The table renders, from the fallback profile.
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })
    await expect(page.getByRole('combobox', { name: /chart/i })).not.toHaveValue(
      '00000000-0000-0000-0000-000000000000',
    )
  })
})
