import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

/**
 * Automated accessibility scan.
 *
 * Three layers already exist and each misses what the others catch:
 * `eslint-plugin-jsx-a11y` reads source and cannot see a computed
 * colour; the contrast unit tests read tokens and cannot see which
 * element ended up on which background; the manual pass in
 * `docs/TESTING-PHASE-0.md` catches what a human notices and nothing
 * else. axe reads the rendered accessibility tree, which is the thing a
 * screen reader actually consumes.
 *
 * It is not a substitute for any of them, and a clean axe run does not
 * mean a page is usable — roughly a third of WCAG cannot be checked
 * automatically. It means the mechanical failures are gone.
 */

const PAGES = [
  { name: 'landing', path: '/' },
  { name: 'status', path: '/status' },
  { name: '404', path: '/no-such-page' },
]

for (const page_ of PAGES) {
  test(`${page_.name} page has no detectable accessibility violations`, async ({
    page,
  }) => {
    await page.goto(page_.path)
    await page.waitForLoadState('networkidle')

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze()

    // Name the offending selectors in the failure. "3 violations" sends
    // someone hunting; "colour-contrast on p.text-ink-faint" does not.
    const summary = results.violations.map((v) => ({
      id: v.id,
      impact: v.impact,
      help: v.help,
      nodes: v.nodes.map((n) => n.target.join(' ')),
    }))

    expect(summary, JSON.stringify(summary, null, 2)).toEqual([])
  })
}

test('the landing page is reachable and operable by keyboard alone', async ({
  page,
}) => {
  await page.goto('/')

  // The skip link must be first: a keyboard user should not have to tab
  // through the whole header to reach content on every navigation.
  await page.keyboard.press('Tab')
  await expect(page.getByRole('link', { name: /skip to content/i })).toBeFocused()

  // Every subsequent stop must show a visible focus indicator. A ring
  // removed by `outline: none` with no replacement is the single most
  // common way a site becomes unusable without a mouse.
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Tab')
    const visible = await page.evaluate(() => {
      const el = document.activeElement
      if (!el || el === document.body) return true // ran out of stops
      const s = getComputedStyle(el)
      const ring = s.getPropertyValue('--tw-ring-shadow')
      return (
        (s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0) ||
        (ring !== '' && ring !== '0 0 #0000') ||
        s.boxShadow !== 'none'
      )
    })
    expect(visible, `focus indicator missing on tab stop ${i + 2}`).toBe(true)
  }
})
