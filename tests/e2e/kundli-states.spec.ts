import { expect, test, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The four states, on every Kundli route.
 *
 * Task 3.18 and the Definition of Done: "Every screen needs four states —
 * loading, error, empty, and populated."
 *
 * ── Why this is an e2e spec and not a source scan ──
 *
 * The obvious cheap version greps each `page.tsx` for a skeleton, an
 * error branch and an empty branch. That approach produced two FALSE
 * NEGATIVES on its first run here: `/onboarding/computing` was reported
 * as having no error path when it has a `failed` state, and
 * `/shared/[token]` as having none when it handles both `gone` and
 * `unreadable` — the grep simply did not know those names.
 *
 * A guard that reports a screen as unprotected when it is protected is
 * one people learn to ignore, and it would say nothing at all about a
 * screen that has a branch which never renders. So this drives the real
 * states in a real browser instead.
 *
 * ── The empty state is the one that rots ──
 *
 * Loading and error get exercised by ordinary use. "No birth profile
 * yet" is seen once, by each user, before they have done anything — and
 * then never again by anyone building the product. It is the state most
 * likely to be broken for months without anyone noticing.
 */

const KUNDLI_ROUTES = [
  '/kundli/chart',
  '/kundli/planets',
  '/kundli/dashas',
  '/kundli/yogas',
  '/kundli/transits',
] as const

/** A phone tail no other spec uses — see mask-letters.spec.ts. */
const PHONE_TAIL = '743'

async function signUpWithoutAProfile(page: Page): Promise<void> {
  const phone = uniquePhone(PHONE_TAIL)
  const otp = watchOTP(phone)

  await page.goto('/auth')
  await page.getByRole('button', { name: /use phone instead/i }).click()
  await page.getByLabel(/phone number/i).fill(phone)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Anandi')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)
}

test.describe.configure({ mode: 'serial' })

test.describe('the empty state', () => {
  let page: Page

  test.beforeAll(async ({ browser }) => {
    page = await browser.newPage()
    // Signed in, and deliberately with NO birth profile. This is the
    // state every user passes through exactly once and nobody building
    // the product ever sees again.
    await signUpWithoutAProfile(page)
  })

  test.afterAll(async () => {
    await page.close()
  })

  for (const route of KUNDLI_ROUTES) {
    test(`${route} explains itself with no birth profile`, async () => {
      await page.goto(route)

      /*
        Something readable, and NOT an error.

        The distinction is the whole point. A user who has not entered
        their birth details has done nothing wrong, and a screen that
        greets them with "something went wrong" or an endless spinner
        reads as the product being broken on their first visit.
      */
      const main = page.locator('main')
      await expect(main).toBeVisible({ timeout: 30_000 })

      // Not still loading thirty seconds later.
      await expect(main.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })

      const text = (await main.textContent()) ?? ''
      expect(text.trim().length, `${route} renders an empty <main> with no profile`)
        .toBeGreaterThan(20)

      expect(text, `${route} shows an ERROR for a user who simply has no profile yet`)
        .not.toMatch(/something went wrong|could not be read/i)

      // And a way forward, rather than a dead end.
      const cta = page.getByRole('link', { name: /birth details|add/i })
        .or(page.getByRole('button', { name: /birth details|add/i }))
      await expect(cta.first(), `${route} offers no way to add birth details`)
        .toBeVisible()
    })
  }
})

test.describe('the loading state', () => {
  /*
    A skeleton, not a spinner on a blank page.

    `.claude/rules/frontend.md`: "A skeleton that looks like the eventual
    content beats a spinner on a blank page." Asserted by holding the
    chart request open and looking at what is on screen while it hangs —
    the only moment the loading state is observable.
  */
  test('the chart screen shows a skeleton while the chart is in flight', async ({
    page,
  }) => {
    const phone = uniquePhone(PHONE_TAIL)
    const otp = watchOTP(phone)

    await page.goto('/auth')
    await page.getByRole('button', { name: /use phone instead/i }).click()
    await page.getByLabel(/phone number/i).fill(phone)
    await page.getByRole('button', { name: /^continue$/i }).click()
    await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())
    await expect(page).toHaveURL(/\/onboarding\/name/)
    await page.getByLabel(/your name/i).fill('Latha')
    await page.getByRole('button', { name: /finish/i }).click()
    await expect(page).toHaveURL(/\/home$/)

    // Hold the chart request open so the loading state stays on screen.
    let release: (() => void) | undefined
    const held = new Promise<void>((resolve) => {
      release = resolve
    })

    await page.route('**/api/v1/charts/**', async (route) => {
      await held
      await route.continue()
    })

    await page.goto('/kundli/chart')

    // While it hangs: a busy region, announced.
    const busy = page.locator('[aria-busy="true"]')
    await expect(busy.first()).toBeVisible({ timeout: 15_000 })

    release?.()
  })
})

test.describe('the error state', () => {
  /*
    An error the user can act on, on every Kundli route.

    `load-errors.spec.ts` covers this for one screen. The rule is that
    EVERY screen has it, and a rule checked in one place is a rule that
    holds in one place.
  */
  let page: Page

  test.beforeAll(async ({ browser }) => {
    page = await browser.newPage()
    await signUpWithoutAProfile(page)
  })

  test.afterAll(async () => {
    await page.close()
  })

  /*
    The PROFILE list, not every endpoint.

    Blanket-failing every `/api/v1/` path was the first attempt and it
    tested
    the wrong thing: it also fails `/api/v1/auth/refresh`, and a session
    that cannot be refreshed genuinely cannot continue — so the app
    correctly redirected to sign-in and the test read that as "no error
    state". `load-errors.spec.ts` scopes to data endpoints for exactly
    this reason.

    Every Kundli route loads the birth-profile list before anything
    else, so failing that one endpoint reaches the error branch on all
    five without touching authentication.
  */
  const DATA_ENDPOINT = '**/api/v1/birth-profiles*'

  for (const route of KUNDLI_ROUTES) {
    test(`${route} shows a retryable error when the API fails`, async () => {
      await page.route(DATA_ENDPOINT, (r) =>
        r.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
      )

      await page.goto(route)

      const main = page.locator('main')
      await expect(main).toBeVisible({ timeout: 30_000 })
      await expect(main.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })

      // Says something went wrong, and offers a way to try again — an
      // error with no action is a dead end.
      const retry = page.getByRole('button', { name: /try again|retry/i })
      await expect(retry.first(), `${route} shows no retry after a 500`).toBeVisible({
        timeout: 15_000,
      })

      // And does NOT sign the user out. A 500 is not a 401, and treating
      // it as one loses their session over a server hiccup.
      expect(page.url(), `${route} redirected to sign-in on a 500`).not.toMatch(/\/auth/)

      await page.unroute(DATA_ENDPOINT)
    })
  }
})
