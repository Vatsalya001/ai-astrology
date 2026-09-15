import { expect, test } from '@playwright/test'

import { codeFor, uniqueEmail } from './otp-log'

/**
 * The §10 auth flows, driven through a real browser against the real
 * stack.
 *
 * The OTP arrives via ConsoleChannel, which writes to the API's stderr —
 * so these read the code out of the log the same way a developer does.
 * That is not a shortcut around the system; it IS the development
 * delivery channel, and reading it proves the whole path works rather
 * than stubbing the one part most likely to break.
 */

test.describe('sign up', () => {
  test('landing → auth → code → onboarding → home', async ({ page }) => {
    const email = uniqueEmail('a') // distinct first letter: see otp-log.ts

    await page.goto('/')
    await page.getByRole('link', { name: /get your free kundli/i }).click()
    await expect(page).toHaveURL(/\/auth$/)

    await page.getByLabel(/email address/i).fill(email)
    await page.getByRole('button', { name: /^continue$/i }).click()
    await expect(page).toHaveURL(/\/auth\/verify/)

    // The identifier is echoed back so the user can check they typed it
    // right before waiting for a code that will never arrive.
    await expect(page.getByText(email)).toBeVisible()

    // Pasting the whole code must fill all six boxes and submit itself.
    await page.locator('input[autocomplete="one-time-code"]').fill(await codeFor(email))

    // A new account goes to onboarding, not straight home.
    await expect(page).toHaveURL(/\/onboarding\/name/)

    await page.getByLabel(/your name/i).fill('Priya')
    await page.getByRole('button', { name: /finish/i }).click()

    await expect(page).toHaveURL(/\/home$/)
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Priya')
  })

  test('a returning user skips onboarding', async ({ page }) => {
    const email = uniqueEmail('b')

    // First pass: create the account.
    await page.goto('/auth')
    await page.getByLabel(/email address/i).fill(email)
    await page.getByRole('button', { name: /^continue$/i }).click()
    const firstCode = await codeFor(email)
    await page.locator('input[autocomplete="one-time-code"]').fill(firstCode)
    await expect(page).toHaveURL(/\/onboarding\/name/)

    await page.getByLabel(/your name/i).fill('Returning')
    await page.getByRole('button', { name: /finish/i }).click()
    await expect(page).toHaveURL(/\/home$/)

    // Sign out, then back in. The second visit must land on /home —
    // sending a returning user through onboarding again is the bug this
    // catches.
    await page.getByRole('button', { name: /sign out/i }).click()
    await expect(page).toHaveURL('/')

    await page.goto('/auth')
    await page.getByLabel(/email address/i).fill(email)
    await page.getByRole('button', { name: /^continue$/i }).click()
    // Explicitly a DIFFERENT code from the first sign-in — otherwise a
    // read that lands before the second request is logged silently
    // reuses the spent one.
    await page
      .locator('input[autocomplete="one-time-code"]')
      .fill(await codeFor(email, { notEqualTo: firstCode }))

    await expect(page).toHaveURL(/\/home$/)
  })
})

test.describe('the verify screen', () => {
  test('rejects a wrong code without leaving the page', async ({ page }) => {
    const email = uniqueEmail('c')

    await page.goto('/auth')
    await page.getByLabel(/email address/i).fill(email)
    await page.getByRole('button', { name: /^continue$/i }).click()
    await expect(page).toHaveURL(/\/auth\/verify/)

    const real = await codeFor(email)
    const wrong = real[0] === '0' ? '1' + real.slice(1) : '0' + real.slice(1)

    await page.locator('input[autocomplete="one-time-code"]').fill(wrong)

    // Scoped to main: Next injects a `__next-route-announcer__` with
    // role="alert" for screen readers on client-side navigation, so an
    // unscoped getByRole('alert') matches two elements — intermittently,
    // depending on whether the announcer currently holds text.
    await expect(page.getByRole('main').getByRole('alert')).toBeVisible()
    await expect(page).toHaveURL(/\/auth\/verify/)

    // The field is cleared so the next attempt starts from empty rather
    // than needing six backspaces.
    await expect(page.locator('input[autocomplete="one-time-code"]')).toHaveValue('')

    // And the correct code still works afterwards — a wrong guess must
    // not burn the code on the first try.
    await page.locator('input[autocomplete="one-time-code"]').fill(real)
    await expect(page).toHaveURL(/\/onboarding\/name/)
  })

  test('offers resend only after a countdown', async ({ page }) => {
    await page.goto('/auth')
    await page.getByLabel(/email address/i).fill(uniqueEmail('d'))
    await page.getByRole('button', { name: /^continue$/i }).click()

    // A countdown, not a bare disabled button: the user needs to know it
    // will come back, and when.
    await expect(page.getByText(/resend in \d+s/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /^resend code$/i })).toHaveCount(0)
  })

  test('sends you back if you arrive with no identifier', async ({ page }) => {
    // A hand-typed URL or a lost navigation. Showing a form that cannot
    // work is worse than returning to the start.
    await page.goto('/auth/verify')
    await expect(page).toHaveURL(/\/auth$/)
  })
})

test.describe('protected routes', () => {
  test('/home redirects to /auth when signed out', async ({ page }) => {
    await page.goto('/home')
    await expect(page).toHaveURL(/\/auth$/)
  })
})

test.describe('consent links resolve', () => {
  // Asking someone to agree to terms behind a 404 is asking them to
  // consent to something they cannot read.
  for (const path of ['/terms', '/privacy']) {
    test(`${path} exists and is readable`, async ({ page }) => {
      const response = await page.goto(path)
      expect(response?.status()).toBe(200)
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
    })
  }
})
