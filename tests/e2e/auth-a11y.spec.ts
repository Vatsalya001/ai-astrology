import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

import { codeFor, uniqueEmail } from './otp-log'

/**
 * Accessibility for the screens that cannot be reached by URL alone.
 *
 * /auth/verify needs a pending code and /onboarding/name needs a session,
 * so a11y.spec.ts cannot just navigate to them. Scanning them from inside
 * the flow is the only way they get checked at all — and they are the two
 * screens every single user passes through.
 */

async function scan(page: Parameters<typeof AxeBuilder>[0]['page'], label: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()

  const summary = results.violations.map((v) => ({
    id: v.id,
    impact: v.impact,
    help: v.help,
    nodes: v.nodes.map((n) => n.target.join(' ')),
  }))
  expect(summary, `${label}:\n${JSON.stringify(summary, null, 2)}`).toEqual([])
}

test('the whole signup flow is free of detectable violations', async ({ page }) => {
  const email = uniqueEmail('e')

  await page.goto('/auth')
  await page.waitForLoadState('networkidle')
  await scan(page, '/auth')

  await page.getByLabel(/email address/i).fill(email)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await expect(page).toHaveURL(/\/auth\/verify/)
  await page.waitForLoadState('networkidle')
  await scan(page, '/auth/verify')

  // The error state too — a message announced badly is worse than no
  // message, and this is the state users actually hit.
  const real = await codeFor(email)
  const wrong = real[0] === '0' ? '1' + real.slice(1) : '0' + real.slice(1)
  await page.locator('input[autocomplete="one-time-code"]').fill(wrong)
  await expect(page.getByRole('main').getByRole('alert')).toBeVisible()
  await scan(page, '/auth/verify (error state)')

  await page.locator('input[autocomplete="one-time-code"]').fill(real)
  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.waitForLoadState('networkidle')
  await scan(page, '/onboarding/name')

  await page.getByLabel(/your name/i).fill('A11y')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)
  await page.waitForLoadState('networkidle')
  await scan(page, '/home')
})

test('the auth screen is operable by keyboard alone', async ({ page }) => {
  await page.goto('/auth')

  const field = page.getByLabel(/email address/i)

  // Tab until the field has focus rather than hardcoding a count. The
  // order today is skip-link → wordmark → field; asserting "two tabs"
  // makes the test fail the next time a header link is added, which is a
  // maintenance cost with no safety benefit. What matters is that the
  // field is reachable WITHOUT a mouse, and quickly.
  const maxTabs = 6
  let reached = false
  for (let i = 0; i < maxTabs; i++) {
    await page.keyboard.press('Tab')
    if (await field.evaluate((el) => el === document.activeElement)) {
      reached = true
      break
    }
  }
  expect(reached, `the email field was not reachable within ${maxTabs} tab stops`).toBe(true)

  // Type and submit with Enter — nobody should need a mouse to sign in.
  await page.keyboard.type(uniqueEmail('f'))
  await page.keyboard.press('Enter')

  await expect(page).toHaveURL(/\/auth\/verify/)
})
