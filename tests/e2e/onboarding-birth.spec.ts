import { expect, test, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * Birth details, browser to Go to Python and back.
 *
 * This is the only suite that exercises the whole Phase 2 path: a form
 * in a browser, a place resolved against the real gazetteer, a UTC
 * instant derived from a real historical timezone, a chart computed by
 * astro-service, and a row in Postgres. Every other test in the repo
 * stubs at one of those boundaries.
 */

/**
 * ONE account for the whole file, created once.
 *
 * Serial rather than parallel, and one sign-up rather than eight, for a
 * reason measured rather than guessed: `OTPRequestPerIP` allows 30
 * requests per 15 minutes from one address, and eight more sign-ups took
 * the whole suite past it. Six tests across three OTHER spec files
 * started failing with "wrong code" — the limiter working exactly as
 * designed, reported as broken authentication.
 *
 * The tests that need an empty profile list get one from `clearProfiles`
 * rather than from a new account. Soft deletion makes that honest: the
 * list really is empty afterwards, which is the state under test.
 */
test.describe.configure({ mode: 'serial' })

let shared: Page

test.beforeAll(async ({ browser }) => {
  shared = await browser.newPage()
  await signUp(shared, 'g')
})

test.afterAll(async () => {
  await shared.close()
})

test.beforeEach(async () => {
  await clearProfiles(shared)
})

/**
 * Removes every profile, through the UI.
 *
 * Through the UI because the access token lives in a module variable in
 * the app — in memory, deliberately, so an XSS bug cannot read it — and
 * that puts it out of reach of `page.request`. Clicking is slower and
 * exercises the delete path as a side effect.
 */
async function clearProfiles(page: Page): Promise<void> {
  await page.goto('/settings/birth-profiles')

  const cards = page.getByRole('article')
  const empty = page.getByText(/add your birth details to get started/i)

  // Wait for the list to have LOADED before counting it. Counting while
  // the fetch is in flight returns zero, and this function then reports
  // "already empty" and leaves the previous test's profiles in place —
  // which surfaces two tests later as a strict-mode violation on a
  // button that should have been unique.
  await expect(cards.first().or(empty)).toBeVisible()

  // Bounded. An unbounded loop against a button that stops disappearing
  // hangs the suite instead of failing it.
  for (let guard = 0; guard < 10; guard++) {
    const remaining = await cards.count()
    if (remaining === 0) return

    // Both clicks scoped to the SAME card. Taking the last matching
    // button on the page instead expands a different card's confirm
    // panel, and the loop then removes nothing while believing it did.
    const card = cards.first()
    await card.getByRole('button', { name: /^remove$/i }).click()
    await card.getByRole('button', { name: /^remove$/i }).click()

    // Wait for the card to actually go, rather than for a fixed delay
    // that is either too short on a slow run or wasted on a fast one.
    await expect(cards).toHaveCount(remaining - 1)
  }
  throw new Error('profiles would not clear; the cards never went away')
}

/** Signs up a fresh account and gets past the name screen. */
async function signUp(page: Page, letter: string): Promise<string> {
  const email = uniqueEmail(letter)
  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Test')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)

  return email
}

/** Fills step 1 and moves to step 2. */
async function enterDate(page: Page, day: string, month: string, year: string): Promise<void> {
  await page.getByLabel(/^day$/i).fill(day)
  await page.getByLabel(/^month$/i).fill(month)
  await page.getByLabel(/^year$/i).fill(year)
  await page.getByRole('button', { name: /^continue$/i }).click()
}

/** Types into the place box and picks the first suggestion. */
async function pickPlace(page: Page, query: string, expected: RegExp): Promise<void> {
  await page.getByLabel(/birth place/i).fill(query)
  const first = page.getByRole('option').first()
  await expect(first).toBeVisible()
  await expect(first).toHaveText(expected)
  await first.click()
}

test.describe('the birth-details flow', () => {
  test('three steps produce a stored profile and a chart', async () => {
    const page = shared

    await page.goto('/onboarding/birth')
    await expect(page.getByRole('heading', { name: /when were you born/i })).toBeVisible()

    await enterDate(page, '17', '8', '1994')

    await expect(page.getByRole('heading', { name: /what time were you born/i })).toBeVisible()
    await page.getByLabel(/^hour$/i).fill('14')
    await page.getByLabel(/^minute$/i).fill('35')
    await page.getByRole('button', { name: /^continue$/i }).click()

    await expect(page.getByRole('heading', { name: /where were you born/i })).toBeVisible()
    await pickPlace(page, 'jaip', /Rajasthan/)

    await page.getByRole('button', { name: /see my kundli/i }).click()

    // The computing screen is a real wait on a real computation, so it
    // may be gone before an assertion lands. Waiting for the URL rather
    // than the heading is the difference between a test and a flake.
    await page.waitForURL(/\/(onboarding\/computing|home)/)
    await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })

    // The profile is really there, with the details entered.
    await page.goto('/settings/birth-profiles')
    await expect(page.getByText(/17 August 1994/)).toBeVisible()
    await expect(page.getByText(/14:35/)).toBeVisible()
    await expect(page.getByText(/Jaipur/)).toBeVisible()
  })

  // The population ranking is the whole reason the search is useful. Two
  // Jaipurs exist; the one with 2.7 million people must come first.
  test('ranks the larger place first', async () => {
    const page = shared
    await page.goto('/onboarding/birth')
    await enterDate(page, '1', '1', '1990')
    await page.getByLabel(/^hour$/i).fill('12')
    await page.getByLabel(/^minute$/i).fill('0')
    await page.getByRole('button', { name: /^continue$/i }).click()

    await page.getByLabel(/birth place/i).fill('jaip')
    const options = page.getByRole('option')
    await expect(options.first()).toBeVisible()

    // Both rows exist; the order is the assertion.
    await expect(options).toHaveCount(2)
    await expect(options.nth(0)).toContainText('Rajasthan')
    await expect(options.nth(1)).toContainText('Odisha')
  })

  // The escape hatch. Without it, somebody who does not know their birth
  // time abandons signup entirely.
  test('an unknown birth time still produces a profile, and says what is missing', async () => {
    const page = shared
    await page.goto('/onboarding/birth')
    await enterDate(page, '23', '4', '1998')

    await page.getByLabel(/don.t know my exact birth time/i).check()

    // Explained, not hidden. The user is told what becomes unavailable.
    await expect(page.getByText(/rising sign and dasha periods need an exact time/i)).toBeVisible()

    await page.getByRole('button', { name: /^continue$/i }).click()
    await pickPlace(page, 'pune', /Pune/)
    await page.getByRole('button', { name: /see my kundli/i }).click()

    await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })

    await page.goto('/settings/birth-profiles')
    await expect(page.getByText(/birth time not set/i)).toBeVisible()
    // Persistent and offers the fix, rather than only naming the gap.
    await expect(page.getByRole('link', { name: /add the time/i })).toBeVisible()
  })

  test('refuses a date that does not exist', async () => {
    const page = shared
    await page.goto('/onboarding/birth')

    // 31 February. `new Date` would roll this to 3 March rather than
    // rejecting it, storing a chart for a day nobody named.
    await enterDate(page, '31', '2', '1994')

    // By id, not by role. Next.js renders its own
    // `<div role="alert" id="__next-route-announcer__">` on every page,
    // so getByRole('alert') is always ambiguous — a strict-mode
    // violation that looks like a missing error message.
    await expect(page.locator('#date-error')).toContainText(/doesn.t exist/i)
    await expect(page.getByRole('heading', { name: /when were you born/i })).toBeVisible()
  })

  test('refuses to continue without a place chosen from the list', async () => {
    const page = shared
    await page.goto('/onboarding/birth')
    await enterDate(page, '1', '1', '1990')
    await page.getByLabel(/^hour$/i).fill('12')
    await page.getByLabel(/^minute$/i).fill('0')
    await page.getByRole('button', { name: /^continue$/i }).click()

    // Typed but not selected. Coordinates come from the gazetteer, so
    // free text is not a place.
    await page.getByLabel(/birth place/i).fill('somewhere')
    await page.getByRole('button', { name: /see my kundli/i }).click()

    await expect(page.locator('#place-error')).toContainText(/choose your birth place/i)
  })
})

test.describe('the profile manager', () => {
  test('is empty before anything is added, and says what to do', async () => {
    const page = shared
    await page.goto('/settings/birth-profiles')

    await expect(page.getByText(/add your birth details to get started/i)).toBeVisible()
    await expect(page.getByRole('link', { name: /add birth details/i })).toBeVisible()
  })

  // Editing creates a new VERSION rather than mutating, so a reading
  // given last month stays attached to the details it was computed from.
  test('editing warns that it creates a version, and does', async () => {
    const page = shared

    await page.goto('/onboarding/birth')
    await enterDate(page, '15', '3', '1990')
    await page.getByLabel(/^hour$/i).fill('6')
    await page.getByLabel(/^minute$/i).fill('30')
    await page.getByRole('button', { name: /^continue$/i }).click()
    await pickPlace(page, 'delhi', /Delhi/)
    await page.getByRole('button', { name: /see my kundli/i }).click()
    await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })

    await page.goto('/settings/birth-profiles')
    await page.getByRole('link', { name: /^edit$/i }).click()

    // The warning is the point of the screen.
    await expect(page.getByRole('note')).toContainText(/creates a new version/i)

    await page.getByLabel(/^hour$/i).fill('7')
    await page.getByLabel(/^minute$/i).fill('45')
    await pickPlace(page, 'delhi', /Delhi/)
    await page.getByRole('button', { name: /^continue$/i }).click()

    await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })

    await page.goto('/settings/birth-profiles')
    await expect(page.getByText(/07:45/)).toBeVisible()
    await expect(page.getByText(/version 2/i)).toBeVisible()
    // Exactly one active profile: the old version is superseded, not a
    // second entry in the list.
    await expect(page.getByRole('article')).toHaveCount(1)
  })

  test('removing says the deletion is soft before doing it', async () => {
    const page = shared

    await page.goto('/onboarding/birth')
    await enterDate(page, '2', '11', '1985')
    await page.getByLabel(/don.t know my exact birth time/i).check()
    await page.getByRole('button', { name: /^continue$/i }).click()
    await pickPlace(page, 'mumbai', /Mumbai/)
    await page.getByRole('button', { name: /see my kundli/i }).click()
    await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })

    await page.goto('/settings/birth-profiles')
    const card = page.getByRole('article').first()
    await card.getByRole('button', { name: /^remove$/i }).click()

    // "Delete" on a screen where deletion is soft is a lie the user
    // finds out about later.
    await expect(page.getByText(/past readings stay readable/i)).toBeVisible()

    await card.getByRole('button', { name: /^remove$/i }).click()
    await expect(page.getByText(/add your birth details to get started/i)).toBeVisible()
  })
})
