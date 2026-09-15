import { expect, test, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * A failed load is not the same as being signed out.
 *
 * Every authenticated screen used to send every load failure to /auth.
 * A dropped connection or an API restart therefore looked exactly like a
 * dead session: the user was thrown to the sign-in page, told nothing,
 * and — if the outage was still going — could not sign in either, so the
 * reasonable conclusion was that their account had gone. That is the
 * failure this product will hit most often, given its users are on mobile
 * networks.
 *
 * Two halves, and both have to hold or the fix is worse than the bug:
 *
 *   500 / network  → show an error with a retry, stay on the page
 *   401            → still redirect, because the session really is gone
 *
 * Testing only the first would pass a version that never redirects, which
 * would strand a signed-out user on a screen that can never load.
 */

const SCREENS = [
  { path: '/home', api: '**/api/v1/users/me' },
  { path: '/settings/profile', api: '**/api/v1/users/me' },
  { path: '/settings/preferences', api: '**/api/v1/users/me/preferences' },
  { path: '/settings/sessions', api: '**/api/v1/users/me/sessions' },
]

async function signUp(page: Page, letter: string): Promise<void> {
  const email = uniqueEmail(letter)

  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Priya')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)
}

test('a 500 shows an error and keeps the user where they are', async ({ page }) => {
  await signUp(page, 'i')

  for (const { path, api } of SCREENS) {
    await page.route(api, (route) =>
      route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } }),
    )

    await page.goto(path)

    await expect(
      page.getByRole('alert').filter({ hasText: /something went wrong/i }),
      `${path} did not show an error`,
    ).toBeVisible()

    // The important half: still here, not bounced to sign-in.
    await expect(page, `${path} redirected to /auth on a 500`).toHaveURL(
      new RegExp(`${path.replace('/', '\\/')}$`),
    )

    await expect(page.getByRole('button', { name: /try again/i })).toBeVisible()
    await page.unroute(api)
  }
})

test('an unreachable server is an error, not a sign-out', async ({ page }) => {
  await signUp(page, 'j')

  // `abort` is what a dropped connection actually looks like to fetch: no
  // response at all, which surfaces as AuthError('NETWORK', 0). Status 0
  // is exactly the case the old code mistook for a dead session.
  await page.route('**/api/v1/users/me', (route) => route.abort('connectionfailed'))

  await page.goto('/home')

  await expect(page.getByRole('alert').filter({ hasText: /something went wrong/i })).toBeVisible()
  await expect(page).toHaveURL(/\/home$/)
})

test('retry recovers once the server is back', async ({ page }) => {
  await signUp(page, 'p')

  let failNext = true
  await page.route('**/api/v1/users/me', async (route) => {
    if (failNext) {
      failNext = false
      await route.fulfill({ status: 500, json: { error: { code: 'INTERNAL_ERROR' } } })
      return
    }
    await route.fallback()
  })

  await page.goto('/settings/profile')
  await expect(page.getByRole('alert')).toBeVisible()

  await page.getByRole('button', { name: /try again/i }).click()

  // A retry that does not actually re-fetch would leave the error up.
  await expect(page.getByLabel(/^name$/i)).toHaveValue('Priya')
})

// The other half. Without this, a version that simply never redirects
// would pass everything above while stranding a signed-out visitor on a
// screen that can never load.
test('a 401 still redirects to sign-in', async ({ page }) => {
  await page.route('**/api/v1/auth/refresh', (route) =>
    route.fulfill({ status: 401, json: { error: { code: 'UNAUTHORIZED' } } }),
  )

  await page.goto('/settings/profile')

  await expect(page).toHaveURL(/\/auth$/)
})
