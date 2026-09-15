import { expect, test, type BrowserContext, type Page } from '@playwright/test'

import { uniqueEmail, uniquePhone, watchOTP } from './otp-log'

/**
 * The two §10 flows that need more than one browser or more than one
 * account state, and the phone channel.
 *
 * They are here rather than in settings.spec.ts because each needs its
 * own `browser` fixture — two isolated contexts are two separate cookie
 * jars, which is the only honest way to test "one device, not the other".
 * A second tab in the same context shares the refresh cookie and would
 * pass whether or not revocation worked.
 */

const API_URL = process.env.API_URL ?? 'http://localhost:4000'

/** Signs in an existing-or-new account in the given context. */
async function signIn(
  page: Page,
  email: string,
  opts: { expectOnboarding?: boolean } = {},
): Promise<void> {
  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  if (opts.expectOnboarding !== false) {
    await expect(page).toHaveURL(/\/onboarding\/name/)
    await page.getByLabel(/your name/i).fill('Test')
    await page.getByRole('button', { name: /finish/i }).click()
  }
  await expect(page).toHaveURL(/\/home$/)
}

test.describe('sessions across two devices', () => {
  // §10: log in on two "devices" → revoke one → that one gets 401.
  //
  // The existing coverage revokes the CURRENT device, which is the easy
  // half: the app signs itself out locally, so it would pass even if the
  // server did nothing. This is the half that actually tests the server —
  // the revoked browser has no idea anything happened until it asks.
  test('revoking the other device ends only that one', async ({ browser }) => {
    const email = uniqueEmail('t')

    // Two contexts, two cookie jars. A second tab in one context shares
    // the refresh cookie and would pass regardless.
    const first = await browser.newContext()
    const second = await browser.newContext()

    try {
      const phone = await first.newPage()
      const laptop = await second.newPage()

      await signIn(phone, email)
      await signIn(laptop, email, { expectOnboarding: false })

      // Two rotation families now, one per context.
      await laptop.goto('/settings/sessions')
      // `:has(button)` excludes the settings nav, which is also a ul of
      // li inside <main> — without it this counts four tab links as
      // devices and the assertion is meaningless.
      const devices = laptop.locator('main ul li:has(button)')
      await expect(devices).toHaveCount(2)

      // Revoke the one that is NOT this browser.
      const other = devices.filter({ hasNotText: /this device/i })
      await expect(other).toHaveCount(1)
      await other.getByRole('button', { name: /^revoke$/i }).click()

      await expect(devices).toHaveCount(1)
      await expect(laptop.getByText(/this device/i)).toBeVisible()

      // The revoked browser still holds an access token in memory, and
      // access tokens are stateless by design — it stays usable until it
      // expires. What must fail immediately is the REFRESH, which is what
      // a page load does.
      await phone.reload()
      await expect(phone).toHaveURL(/\/auth$/, { timeout: 15_000 })

      // And the laptop is unaffected. Revoking one device must not be a
      // sign-out everywhere.
      await laptop.reload()
      await expect(laptop).toHaveURL(/\/settings\/sessions$/)
    } finally {
      await first.close()
      await second.close()
    }
  })

  // The revoked refresh cookie must be refused by the API itself, not
  // merely ignored by the front end.
  test('a revoked device cannot mint a new access token', async ({ browser }) => {
    const email = uniqueEmail('u')

    const first = await browser.newContext()
    const second = await browser.newContext()

    try {
      const phone = await first.newPage()
      const laptop = await second.newPage()

      await signIn(phone, email)
      await signIn(laptop, email, { expectOnboarding: false })

      await laptop.goto('/settings/sessions')
      const devices = laptop.locator('main ul li:has(button)')
      await expect(devices).toHaveCount(2)

      const other = devices.filter({ hasNotText: /this device/i })
      await other.getByRole('button', { name: /^revoke$/i }).click()
      await expect(devices).toHaveCount(1)

      // Asked of the API directly, carrying the revoked context's cookie.
      const refused = await first.request.post(`${API_URL}/api/v1/auth/refresh`, {
        data: {},
        failOnStatusCode: false,
      })
      expect(refused.status()).toBe(401)

      // The still-live context must still work, or the test proves only
      // that refresh is broken for everyone.
      const accepted = await second.request.post(`${API_URL}/api/v1/auth/refresh`, {
        data: {},
        failOnStatusCode: false,
      })
      expect(accepted.status()).toBe(200)
    } finally {
      await first.close()
      await second.close()
    }
  })
})

test.describe('deletion', () => {
  // §10: settings → delete → OTP → login blocked.
  //
  // "Blocked" needs care. The account is marked deleted and every session
  // is revoked immediately, but the rows survive the grace window so the
  // request stays cancellable. What must be true right away is that the
  // existing sessions are dead.
  test('requesting deletion ends every session immediately', async ({ browser }) => {
    const email = uniqueEmail('v')

    const first = await browser.newContext()
    const second = await browser.newContext()

    try {
      const phone = await first.newPage()
      const laptop = await second.newPage()

      await signIn(phone, email)
      await signIn(laptop, email, { expectOnboarding: false })

      await laptop.goto('/settings/delete')

      // Armed BEFORE the click that sends it. The offset half of the
      // technique only excludes history if it is taken first.
      const otp = watchOTP(email)
      await laptop.getByRole('button', { name: /send me a code/i }).click()

      await laptop.locator('#code').fill(await otp.next())
      await laptop.locator('#confirm').fill('DELETE')
      await laptop.getByRole('button', { name: /delete my account/i }).click()

      // Both browsers lose their session, not just the one that asked.
      await phone.reload()
      await expect(phone).toHaveURL(/\/auth$/, { timeout: 15_000 })

      // And the API refuses the refresh outright.
      const refused = await first.request.post(`${API_URL}/api/v1/auth/refresh`, {
        data: {},
        failOnStatusCode: false,
      })
      expect(refused.status()).toBe(401)
    } finally {
      await first.close()
      await second.close()
    }
  })
})

test.describe('phone channel', () => {
  // Gate item: "Phone OTP works end to end via ConsoleChannel."
  //
  // There is no SMS provider — the spec says so and says why (no free
  // tier). ConsoleChannel is the real delivery path in development for
  // both channels, so this exercises everything except the wire.
  test('a phone number signs up and lands on onboarding', async ({ page }) => {
    // The phone mask keeps only three leading characters and three
    // trailing digits, so the distinct part has to be the tail.
    const phone = uniquePhone('317')

    await page.goto('/auth')
    await page.getByRole('button', { name: /use phone instead/i }).click()

    const field = page.getByLabel(/phone number/i)
    await expect(field).toBeVisible()

    const otp = watchOTP(phone)
    await field.fill(phone)
    await page.getByRole('button', { name: /^continue$/i }).click()

    await expect(page).toHaveURL(/\/auth\/verify/)
    await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

    await expect(page).toHaveURL(/\/onboarding\/name/)
    await page.getByLabel(/your name/i).fill('Phone User')
    await page.getByRole('button', { name: /finish/i }).click()
    await expect(page).toHaveURL(/\/home$/)
  })

  test('an invalid number is refused before a round trip', async ({ page }) => {
    await page.goto('/auth')
    await page.getByRole('button', { name: /use phone instead/i }).click()

    // No country code. The client check exists to catch a typo before a
    // round trip; the server re-validates regardless.
    await page.getByLabel(/phone number/i).fill('9876543210')
    await page.getByRole('button', { name: /^continue$/i }).click()

    // #identifier-error, not getByRole('alert'): Next's
    // __next-route-announcer__ also carries role="alert", so the role
    // selector matches two elements and fails on strict mode.
    await expect(page.locator('#identifier-error')).toContainText(/country code/i)
    await expect(page).toHaveURL(/\/auth$/)
  })
})
