import { devices, expect, test, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * The app at phone width.
 *
 * "Mobile responsive" is in the Definition of Done, and until this file
 * existed nothing tested it — the suite ran one project, Desktop Chrome.
 * That is the wrong gap for a product whose audience is on Indian mobile
 * networks, and it is the same shape of mistake as a guard that is never
 * observed to fire.
 *
 * `test.use` rather than a second Playwright project, deliberately: a
 * project would re-run all 46 specs at a second viewport and roughly
 * double CI for very little extra signal. What actually breaks at phone
 * width is layout and hit targets, so that is what this measures, across
 * every screen including the ones that need a session.
 */

test.use({ ...devices['Pixel 7'] })

const PUBLIC_PAGES = ['/', '/auth', '/terms', '/privacy']
const SIGNED_IN_PAGES = [
  '/home',
  '/settings/profile',
  '/settings/preferences',
  '/settings/sessions',
  '/settings/delete',
]

/** Signs up, so the authenticated screens can be reached. */
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

/**
 * Horizontal overflow is the one layout bug that makes an app feel
 * broken rather than ugly: the page rocks sideways under the thumb and
 * content sits off-screen with no indication it is there.
 *
 * The reference width is Playwright's `viewportSize()`, NOT the page's
 * own `window.innerWidth`. That distinction is the whole test:
 *
 *   Under mobile emulation Chrome EXPANDS the layout viewport to fit
 *   overflowing content, so `innerWidth` grows in lockstep with
 *   `scrollWidth` and the two are always equal. Comparing them to each
 *   other can never fail. Measured: a deliberate 900px element inside a
 *   412px device reported scrollWidth 924 and innerWidth 924, and the
 *   assertion passed while the page was visibly broken.
 *
 * `viewportSize()` comes from the harness rather than the page, so it
 * stays pinned at the device width and the comparison means something.
 */
async function assertNoSidewaysScroll(page: Page, path: string): Promise<void> {
  const device = page.viewportSize()
  expect(device, 'no viewport — mobile emulation is not applied').not.toBeNull()

  const content = await page.evaluate(() => document.documentElement.scrollWidth)

  // One pixel of slack for sub-pixel rounding in the layout engine.
  expect(
    content,
    `${path}: content is ${content}px wide on a ${device!.width}px device — it scrolls sideways`,
  ).toBeLessThanOrEqual(device!.width + 1)
}

test('no public page scrolls sideways on a phone', async ({ page }) => {
  for (const path of PUBLIC_PAGES) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await assertNoSidewaysScroll(page, path)
  }
})

// These need a session, which is why the earlier throwaway check could
// not reach them — and they are the denser screens, so they are the ones
// more likely to overflow.
test('no signed-in screen scrolls sideways on a phone', async ({ page }) => {
  await signUp(page, 'y')

  for (const path of SIGNED_IN_PAGES) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await assertNoSidewaysScroll(page, path)
  }
})

/**
 * Tap targets, with the exemptions WCAG 2.2 actually grants.
 *
 * SC 2.5.8 sets a 24x24 minimum and then exempts two things this app
 * relies on: targets inline in a sentence, and targets that are not
 * visible. Asserting the raw minimum without those exemptions flags the
 * consent-line links and the skip link — both correct as they are — and
 * a check that cries wolf gets deleted.
 */
test('every tappable control is big enough, or exempt for a stated reason', async ({ page }) => {
  await page.goto('/auth')

  const tooSmall: string[] = []

  for (const control of await page.locator('button, a[href], input, select').all()) {
    if (!(await control.isVisible())) continue // SC 2.5.8 exempts hidden targets

    // SC 2.5.8 exempts a target inline in a block of text — the Terms and
    // Privacy links inside the consent sentence.
    const inline = await control.evaluate((el) => {
      const parent = el.parentElement
      if (!parent) return false
      return parent.textContent!.trim().length > (el.textContent ?? '').trim().length + 10
    })
    if (inline) continue

    const box = await control.boundingBox()
    if (box && (box.width < 24 || box.height < 24)) {
      const html = await control.evaluate((el) => el.outerHTML.slice(0, 80))
      tooSmall.push(`${Math.round(box.width)}x${Math.round(box.height)} — ${html}`)
    }
  }

  expect(tooSmall, tooSmall.join('\n')).toEqual([])
})

/**
 * The OTP boxes are the single most important mobile interaction in the
 * product: every user passes through them, on a phone, with a keyboard
 * covering half the screen.
 */
test('the OTP input is usable with a phone keyboard', async ({ page }) => {
  const email = uniqueEmail('z')

  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await expect(page).toHaveURL(/\/auth\/verify/)

  await assertNoSidewaysScroll(page, '/auth/verify')

  const field = page.locator('input[autocomplete="one-time-code"]')

  // inputMode drives which keyboard the OS offers. Without it the user
  // gets a full QWERTY for a six-digit number.
  await expect(field).toHaveAttribute('inputmode', 'numeric')
  // autocomplete="one-time-code" is what lets iOS and Android offer the
  // code from the notification instead of making the user switch apps.
  await expect(field).toHaveAttribute('autocomplete', 'one-time-code')

  await field.fill(await otp.next())
  await expect(page).toHaveURL(/\/onboarding\/name/)
})
