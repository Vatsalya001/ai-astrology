import { expect, test } from '@playwright/test'

/**
 * The accessibility inspector.
 *
 * It is a developer tool, so the bar is different from product code —
 * but two of its properties are worth pinning, because breaking either
 * makes it actively misleading rather than merely absent:
 *
 *   1. It must not ship. A dev panel that reaches users is a bug, and
 *      one that quietly enters the first-load bundle costs every user on
 *      a 4G connection for nobody's benefit.
 *   2. It must not appear in its own focus log. A tool that counts
 *      itself gives the wrong answer to the one question it exists to
 *      answer — "how many stops to reach X".
 */

test('it is absent unless the URL asks for it', async ({ page }) => {
  await page.goto('/auth')
  // Given a moment to lazily mount, in case it were going to.
  await page.waitForTimeout(1000)

  await expect(
    page.locator('[data-a11y-inspector]'),
    'the developer panel rendered for an ordinary visitor',
  ).toHaveCount(0)
})

test('?a11y=1 mounts it, and it reports what is focused', async ({ page }) => {
  await page.goto('/auth?a11y=1')

  const panel = page.locator('[data-a11y-inspector]')
  await expect(panel).toBeVisible({ timeout: 15_000 })

  // A jump button, so the tester never has to "click on empty dark
  // space" to put focus in the page.
  await panel.getByRole('button', { name: 'Main content' }).click()
  await page.keyboard.press('Tab')
  await page.waitForTimeout(400)

  const text = (await panel.textContent()) ?? ''

  expect(text, 'the panel shows no focused element').toContain('Focused now')

  /*
    A real accessible name, not a tag name.

    The email field's name comes from its <label>, which is the case the
    `accessibleName` helper is most likely to get wrong — aria-label and
    text content are easy; a label element's `for` is the one that needs
    a lookup.
  */
  expect(
    text,
    'the panel did not resolve the email input\'s label — it reads names ' +
      'from aria-label, aria-labelledby, <label for>, then text content, ' +
      'and this case exercises the third',
  ).toMatch(/Email address/i)
})

test('it does not count itself in the focus order', async ({ page }) => {
  await page.goto('/auth?a11y=1')
  const panel = page.locator('[data-a11y-inspector]')
  await expect(panel).toBeVisible({ timeout: 15_000 })

  // Click two of the panel's OWN buttons. Neither is part of the page
  // under test, so neither may appear in the log.
  await panel.getByRole('button', { name: 'Main content' }).click()
  await panel.getByRole('button', { name: 'Reset' }).click()
  await page.waitForTimeout(300)

  const text = (await panel.textContent()) ?? ''
  const count = Number((text.match(/Focus order \((\d+)\)/) ?? [])[1] ?? -1)

  expect(
    count,
    'the panel logged its own controls as focus stops. "How many tabs to ' +
      'reach the chart" is the number this tool exists to report, and it ' +
      'is wrong the moment the tool counts itself.',
  ).toBe(0)
})
