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

  test('the sign-in path is live', async ({ page }) => {
    await page.goto('/')
    // Phase 0 shipped these disabled. Phase 1 makes them work, and this
    // test changed with the behaviour rather than being deleted — a
    // removed test is indistinguishable from a forgotten one.
    await page.getByRole('link', { name: /sign in/i }).click()
    await expect(page).toHaveURL(/\/auth$/)
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
    const cta = page.getByRole('link', { name: /get your free kundli/i })
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

test.describe('404', () => {
  test('returns a real 404 status, not a 200 with sad text', async ({ page }) => {
    const response = await page.goto('/no-such-page')
    expect(response?.status()).toBe(404)
  })

  test('is branded and offers a way out', async ({ page }) => {
    await page.goto('/no-such-page')

    // Without app/not-found.tsx, Next serves its own monochrome default.
    // That page has no wordmark and no navigation, so a mistyped URL
    // becomes a dead end that looks nothing like the product.
    await expect(page).toHaveTitle(/Page not found/)
    await expect(
      page.getByRole('heading', { name: /isn't written yet/i }),
    ).toBeVisible()

    await page.getByRole('link', { name: /back to home/i }).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText(
      'It knows your astrology',
    )
  })
})

test('each page reports its own title', async ({ page }) => {
  // Every route inherited the root layout's title until /status declared
  // its own, so an ops dashboard advertised itself as a consumer product.
  await page.goto('/')
  const landing = await page.title()

  await page.goto('/status')
  const status = await page.title()

  expect(status).not.toBe(landing)
  expect(status).toMatch(/System status/)
})

test('health never returns internal topology to the browser', async ({
  request,
}) => {
  // /health is unauthenticated by necessity — a load balancer cannot
  // present a credential — so its body is public. It used to carry
  // `dial tcp 127.0.0.1:8025: connect: connection refused`, which hands
  // over the internal host and port. The detail belongs in the log.
  const res = await request.get(
    (process.env.API_URL ?? 'http://localhost:4000') + '/health',
  )
  expect(res.ok()).toBeTruthy()

  const body = await res.text()
  for (const forbidden of ['dial tcp', '127.0.0.1', 'connection refused']) {
    expect(body).not.toContain(forbidden)
  }

  // Any reason present must come from the closed vocabulary.
  const parsed = JSON.parse(body) as {
    checks: Record<string, { reason?: string }>
  }
  for (const [name, check] of Object.entries(parsed.checks)) {
    if (check.reason) {
      expect(
        ['timeout', 'unreachable', 'unavailable'],
        `${name} reported an unrecognised reason`,
      ).toContain(check.reason)
    }
  }
})
