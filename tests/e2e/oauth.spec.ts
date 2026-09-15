import AxeBuilder from '@axe-core/playwright'
import { test, expect } from '@playwright/test'

// The API, not the web app. `baseURL` points at Next; these assertions
// are about the Go service's own replies.
const API_URL = process.env.API_URL ?? 'http://localhost:4000'

/**
 * The Google button, in both of its states.
 *
 * No Google credentials exist in development or CI, so the real consent
 * screen is unreachable — and it always will be for a test, since it is
 * an interactive page on someone else's domain. What IS testable, and
 * what actually breaks, is the front end's half: does the button appear
 * when the API says the provider is configured, does it stay away when
 * it is not, and does it point at the right place.
 *
 * `/auth/providers` is intercepted rather than reconfigured, so these
 * tests need no credentials and no server restart.
 */

test.describe('Google sign-in button', () => {
  test('is absent when the API reports no provider', async ({ page }) => {
    // Armed BEFORE the navigation. Registering the wait afterwards is a
    // race the test loses: the response has already arrived by the time
    // the form is visible, and the listener then waits for a second one
    // that never comes.
    const providers = page.waitForResponse((r) => r.url().includes('/auth/providers'))

    // The real API, unmodified: no credentials are configured here.
    await page.goto('/auth')
    await expect(page.getByLabel('Email address')).toBeVisible()

    // "Absent" is only meaningful once the answer is in.
    expect(await (await providers).json()).toEqual({ google: false })

    await expect(page.getByRole('link', { name: /continue with google/i })).toHaveCount(0)

    // Email sign-in is unaffected — the point of hiding the button is
    // that nothing else about the page changes.
    await expect(page.getByRole('button', { name: /continue/i })).toBeVisible()
  })

  test('appears and points at the API when the provider is configured', async ({ page }) => {
    await page.route('**/api/v1/auth/providers', (route) =>
      route.fulfill({ json: { google: true } }),
    )

    await page.goto('/auth')

    const button = page.getByRole('link', { name: /continue with google/i })
    await expect(button).toBeVisible()

    // The href must reach the API, not the web app. Sending the browser
    // to a Next route would 404 instead of starting the flow.
    const href = await button.getAttribute('href')
    expect(href).toContain('/api/v1/auth/oauth/google')
    expect(href).not.toContain('accounts.google.com')

    // A real anchor, so it works with middle-click, keyboard and with
    // JavaScript still loading. A button with an onClick would not.
    await expect(button).toHaveJSProperty('tagName', 'A')
  })

  test('the divider does not announce itself to a screen reader', async ({ page }) => {
    await page.route('**/api/v1/auth/providers', (route) =>
      route.fulfill({ json: { google: true } }),
    )
    await page.goto('/auth')

    // "or" between two rules is a visual separator. Read aloud between
    // the two real choices it is noise, so it is aria-hidden.
    await expect(page.getByRole('link', { name: /continue with google/i })).toBeVisible()
    const or = page.locator('[aria-hidden="true"]', { hasText: /^or$/ })
    await expect(or.first()).toBeAttached()
  })

  // The button only exists in this state, so the default a11y sweep in
  // a11y.spec.ts never sees it. Contrast on a bordered control over the
  // navy page, and the accessible name of an anchor whose only child is
  // an aria-hidden SVG, are exactly what that sweep would have caught.
  test('the configured page is free of detectable violations', async ({ page }) => {
    await page.route('**/api/v1/auth/providers', (route) =>
      route.fulfill({ json: { google: true } }),
    )
    await page.goto('/auth')
    await expect(page.getByRole('link', { name: /continue with google/i })).toBeVisible()

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze()

    const summary = results.violations.map((v) => ({
      id: v.id,
      impact: v.impact,
      help: v.help,
      nodes: v.nodes.map((n) => n.target.join(' ')),
    }))
    expect(summary, JSON.stringify(summary, null, 2)).toEqual([])
  })

  // The colour classes on this button are the kind Tailwind drops
  // silently when a token name is wrong — `tsc` sees a valid string and
  // the build succeeds, leaving a transparent control on a navy page.
  // Only a computed style notices.
  test('the button uses real design tokens, not invented class names', async ({ page }) => {
    await page.route('**/api/v1/auth/providers', (route) =>
      route.fulfill({ json: { google: true } }),
    )
    await page.goto('/auth')

    const button = page.getByRole('link', { name: /continue with google/i })
    const styles = await button.evaluate((el) => {
      const s = getComputedStyle(el)
      return { background: s.backgroundColor, border: s.borderTopColor, width: s.borderTopWidth }
    })

    // colors.surface — #141B35. A dropped class leaves rgba(0, 0, 0, 0).
    expect(styles.background).toBe('rgb(20, 27, 53)')
    // colors.border — #252F52, via the `input` semantic token.
    expect(styles.border).toBe('rgb(37, 47, 82)')
    expect(styles.width).not.toBe('0px')
  })

  test('a cancelled sign-in returns the user to a usable page', async ({ page }) => {
    // This is the real callback behaviour: a declined consent screen
    // comes back as ?error=, and the API redirects here.
    await page.goto('/auth?error=cancelled')

    const alert = page.getByRole('alert').filter({ hasText: /cancelled/i })
    await expect(alert).toBeVisible()

    // Crucially, still signable-in. An error page that strands the user
    // is worse than the error.
    await expect(page.getByLabel('Email address')).toBeEditable()
  })

  test('the API refuses the flow outright when it is not configured', async ({ request }) => {
    const res = await request.get(`${API_URL}/api/v1/auth/oauth/google`, { maxRedirects: 0 })

    // 501, not 503: "not implemented here" is permanent, and a 503 tells
    // a well-behaved client to retry forever.
    expect(res.status()).toBe(501)

    const body = await res.json()
    expect(body.error.code).toBe('OAUTH_NOT_CONFIGURED')
    expect(body.error.retryable).toBeUndefined()

    // It must not name an environment variable — that is a hint about
    // the deployment to anyone probing it.
    expect(JSON.stringify(body)).not.toMatch(/GOOGLE_CLIENT|client_secret/i)
  })

  test('a forged callback is refused without saying why', async ({ request }) => {
    const res = await request.get(
      `${API_URL}/api/v1/auth/oauth/google/callback?code=stolen&state=forged`,
      { maxRedirects: 0 },
    )

    expect(res.status()).toBe(401)

    // Every failure reads identically. Telling the caller whether the
    // STATE or the CODE was wrong helps only someone probing.
    const body = JSON.stringify(await res.json())
    expect(body).not.toMatch(/state|code is|expired|verified/i)
  })
})
