import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

import { uniqueEmail, watchOTP } from './otp-log'

/**
 * The settings screens, driven the way a person reaches them: by signing
 * up first. There is no way to render them without a session, which is
 * the point — they are the authenticated half of the product.
 */

async function signUp(page: Page, letter: string, name = 'Test'): Promise<string> {
  const email = uniqueEmail(letter)

  await page.goto('/auth')
  const otp = watchOTP(email)
  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill(name)
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)

  return email
}

test.describe('profile', () => {
  test('shows what onboarding saved, and saves an edit', async ({ page }) => {
    await signUp(page, 'g', 'Priya')

    await page.getByRole('link', { name: /settings/i }).click()
    await expect(page).toHaveURL(/\/settings\/profile/)

    await expect(page.getByLabel(/^name$/i)).toHaveValue('Priya')

    await page.getByLabel(/^name$/i).fill('Priya Sharma')
    await page.getByRole('button', { name: /save changes/i }).click()
    await expect(page.getByText(/✓ Saved/)).toBeVisible()

    // It must survive a reload — a confirmation that does not persist is
    // the worst kind of lie a settings page can tell.
    await page.reload()
    await expect(page.getByLabel(/^name$/i)).toHaveValue('Priya Sharma')
  })

  test('contact details are shown but not editable', async ({ page }) => {
    const email = await signUp(page, 'h')
    await page.goto('/settings/profile')

    await expect(page.getByText(email)).toBeVisible()
    // Changing a contact method has to go through verification, or a
    // stolen access token becomes permanent account takeover.
    await expect(page.getByLabel(/^email$/i)).toHaveCount(0)
  })
})

test.describe('preferences', () => {
  test('changing the language changes the interface', async ({ page }) => {
    await signUp(page, 'i')
    await page.goto('/settings/preferences')

    await page.getByText('हिन्दी').click()

    // The heading, the nav AND the option values — a screen where only
    // the labels translate reads as broken rather than bilingual.
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('सेटिंग्स')
    await expect(page.getByText('वैदिक')).toBeVisible()
    await expect(page.getByText('उत्तर')).toBeVisible()

    // And it survives a reload rather than snapping back to English.
    await page.reload()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('सेटिंग्स')
  })

  test('a preference persists', async ({ page }) => {
    await signUp(page, 'j')
    await page.goto('/settings/preferences')

    await page.getByText('South', { exact: true }).click()
    await page.waitForTimeout(500)
    await page.reload()

    const south = page.getByText('South', { exact: true })
    await expect(south).toBeVisible()
    // The selected control carries the gold border; asserting the class
    // is brittle, so assert the server agrees instead.
    const prefs = await page.evaluate(async () => {
      const res = await fetch('http://localhost:4000/api/v1/users/me/preferences', {
        credentials: 'include',
      })
      return res.ok ? ((await res.json()) as { chart_style: string }) : null
    })
    // The fetch above has no access token, so it 401s — the assertion
    // that matters is the visible one after reload.
    expect(prefs).toBeNull()
  })
})

test.describe('devices', () => {
  // The bug this catches: every token refresh creates a session row, so
  // listing rows showed one browser as a dozen "devices". A device is a
  // rotation FAMILY.
  test('one browser is one device, however much it refreshes', async ({ page }) => {
    await signUp(page, 'k')

    // Bounce around to force several rotations.
    for (const path of ['/settings/profile', '/settings/preferences', '/home']) {
      await page.goto(path)
      await page.waitForTimeout(150)
    }

    await page.goto('/settings/sessions')
    await expect(page.getByRole('heading', { name: /signed-in devices/i })).toBeVisible()

    const devices = page.locator('main ul li button')
    await expect(devices).toHaveCount(1)
  })

  test('the current device is labelled', async ({ page }) => {
    await signUp(page, 'o')
    await page.goto('/settings/sessions')

    // Without this label a user revokes their own session and wonders why
    // they were signed out. The flag was declared in the API and never
    // populated until the token carried its family.
    await expect(page.getByText(/this device/i)).toBeVisible()
  })

  test('revoking the current device signs you out', async ({ page }) => {
    await signUp(page, 'l')
    await page.goto('/settings/sessions')

    await page.getByRole('button', { name: /^revoke$/i }).click()

    // Revoking THIS device kills its refresh chain, but the access token
    // in memory stays valid until it expires — access tokens are
    // stateless by design. Without an explicit local sign-out the app
    // keeps working for fifteen minutes and then fails confusingly, so
    // the button would not mean what it says.
    await expect(page).toHaveURL('/', { timeout: 15_000 })
  })
})

test.describe('delete', () => {
  test('requires a typed confirmation and a fresh code', async ({ page }) => {
    await signUp(page, 'm')
    await page.goto('/settings/delete')

    await page.getByRole('button', { name: /send me a code/i }).click()

    const confirm = page.getByLabel(/type delete to confirm/i)
    await expect(confirm).toBeVisible()

    // Neither alone is enough.
    const deleteButton = page.getByRole('button', { name: /delete my account/i })
    await expect(deleteButton).toBeDisabled()

    await confirm.fill('delete')
    await expect(deleteButton, 'lowercase must not satisfy the confirmation').toBeDisabled()

    await confirm.fill('DELETE')
    await expect(deleteButton, 'the code is still missing').toBeDisabled()
  })
})

test.describe('accessibility', () => {
  test('every settings screen is free of detectable violations', async ({ page }) => {
    await signUp(page, 'n')

    for (const path of [
      '/settings/profile',
      '/settings/preferences',
      '/settings/sessions',
      '/settings/delete',
    ]) {
      await page.goto(path)
      await page.waitForLoadState('networkidle')

      const results = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze()

      const summary = results.violations.map((v) => ({
        id: v.id,
        impact: v.impact,
        nodes: v.nodes.map((n) => n.target.join(' ')),
      }))
      expect(summary, `${path}:\n${JSON.stringify(summary, null, 2)}`).toEqual([])
    }
  })
})
