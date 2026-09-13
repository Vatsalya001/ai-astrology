import { expect, test } from '@playwright/test'

/**
 * Phase 0 smoke tests.
 *
 * Deliberately shallow: Phase 0 has no product behaviour to assert. What
 * these DO cover is the seam nothing else covers — the browser actually
 * reaching the Go API and rendering what it returns.
 */

test.describe('landing page', () => {
  test('renders the hero and the design tokens are applied', async ({ page }) => {
    await page.goto('/')

    await expect(page.getByRole('heading', { level: 1 })).toContainText(
      'It knows your astrology',
    )

    // The midnight-navy background proves Tailwind compiled and the
    // token layer is wired — a broken build often still renders text.
    const bg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor,
    )
    expect(bg).toBe('rgb(11, 16, 38)') // #0B1026

    await expect(page).toHaveTitle(/Ayana/)
  })

  test('pre-launch actions are disabled, not broken links', async ({ page }) => {
    await page.goto('/')
    // Phase 1 enables sign-in; until then it must be visibly unavailable
    // rather than a link to a 404.
    await expect(page.getByRole('button', { name: /sign in/i })).toBeDisabled()
  })

  test('shadcn components resolve the design tokens, not a stray palette', async ({
    page,
  }) => {
    await page.goto('/')

    // This is the one gate that can catch an unresolved Tailwind token.
    // A class naming a colour that does not exist in tailwind.config.ts
    // is dropped silently: `tsc` sees a valid string, the build succeeds,
    // and the component renders with no background at all. Only a
    // computed style says which actually happened.
    const cta = page.getByRole('button', { name: /get your free kundli/i })
    await expect(cta).toBeVisible()

    const styles = await cta.evaluate((el) => {
      const s = getComputedStyle(el)
      return { background: s.backgroundColor, color: s.color }
    })

    expect(styles.background).toBe('rgb(212, 168, 87)') // gold   — primary
    expect(styles.color).toBe('rgb(11, 16, 38)') //        navy   — primary-foreground
  })

  test('is keyboard navigable from the first tab stop', async ({ page }) => {
    await page.goto('/')
    await page.keyboard.press('Tab')
    // The skip link exists so keyboard users can bypass the header.
    await expect(page.getByRole('link', { name: /skip to content/i })).toBeFocused()
  })
})

test.describe('status page', () => {
  test('reads live data from the Go API', async ({ page }) => {
    await page.goto('/status')

    await expect(page.getByRole('heading', { name: 'System status' })).toBeVisible()

    // If the API were unreachable the page renders an error card instead,
    // so this assertion is what proves the browser→API path works.
    await expect(page.getByText('All systems operational')).toBeVisible({
      timeout: 15_000,
    })
  })

  test('reports every dependency individually', async ({ page }) => {
    await page.goto('/status')

    // "The API is down" is not actionable with six dependencies, which is
    // why each is listed separately.
    for (const name of [
      'PostgreSQL',
      'Redis',
      'astro-service',
      'ai-service',
      'Object storage',
      'Mail',
    ]) {
      await expect(page.getByText(name, { exact: true })).toBeVisible()
    }
  })

  test('shows feature flags, all off in Phase 0', async ({ page }) => {
    await page.goto('/status')
    await expect(page.getByText('ai_chat')).toBeVisible()
    await expect(page.getByText(/Everything is off in Phase 0/)).toBeVisible()
  })
})

test('unknown routes render a 404 page rather than crashing', async ({ page }) => {
  const response = await page.goto('/no-such-page')
  expect(response?.status()).toBe(404)
})
