import { expect, test } from '@playwright/test'

/**
 * The Content Security Policy, and whether the app survives it.
 *
 * A CSP is the one security header that can silently break the product.
 * The other four either do nothing visible or block something obviously
 * wrong; this one can stop the app hydrating, leaving a page that renders
 * and does not respond — which looks like a React bug rather than a
 * header.
 *
 * So there are two assertions here and both matter: the policy is
 * present and restrictive, AND the app still works under it.
 */

const DIRECTIVES = [
  "default-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "frame-ancestors 'none'",
  "form-action 'self'",
]

test('every page carries a restrictive policy', async ({ request }) => {
  for (const path of ['/', '/auth', '/terms', '/privacy']) {
    const res = await request.get(path)
    const csp = res.headers()['content-security-policy']

    expect(csp, `${path} has no Content-Security-Policy`).toBeTruthy()
    for (const directive of DIRECTIVES) {
      expect(csp, `${path} is missing ${directive}`).toContain(directive)
    }

    // A wildcard default-src is a policy that permits everything while
    // appearing to be a policy.
    expect(csp).not.toContain('default-src *')
  }
})

test('the API and the web app both refuse to be framed', async ({ request }) => {
  const web = await request.get('/')
  expect(web.headers()['x-frame-options']).toBe('DENY')
  expect(web.headers()['content-security-policy']).toContain("frame-ancestors 'none'")

  const api = await request.get(`${process.env.API_URL ?? 'http://localhost:4000'}/health`)
  expect(api.headers()['x-frame-options']).toBe('DENY')
})

// THE test. A policy the app cannot run under gets deleted by whoever
// hits it on a Friday, so proving the app works under it is what makes
// the policy durable.
test('the app hydrates and stays interactive under the policy', async ({ page }) => {
  const violations: string[] = []
  page.on('console', (message) => {
    if (/Content Security Policy|Refused to (execute|load|connect)/i.test(message.text())) {
      violations.push(message.text())
    }
  })

  await page.goto('/auth')

  // Interactivity is the proof. A blocked hydration script leaves a page
  // that renders correctly and does nothing when clicked — so asserting
  // on the markup would pass while the app was broken.
  await page.getByRole('button', { name: /use phone instead/i }).click()
  await expect(page.getByLabel(/phone number/i)).toBeVisible()

  // And a real API call, which is what connect-src governs.
  await page.getByLabel(/phone number/i).fill('+919876543210')

  expect(violations, `CSP blocked something:\n${violations.join('\n')}`).toEqual([])
})

test('the policy allows the API and nothing else', async ({ request }) => {
  const csp = (await request.get('/')).headers()['content-security-policy'] ?? ''

  const connect = csp.split(';').find((d) => d.trim().startsWith('connect-src')) ?? ''
  expect(connect).toContain(process.env.API_URL ?? 'http://localhost:4000')

  // The web app talks only to the Go API. astro-service and ai-service
  // are internal and must not become browser-reachable by accident.
  expect(connect).not.toContain('8100')
  expect(connect).not.toContain('8200')
  expect(connect.trim()).not.toBe('connect-src *')
})
