import { expect, test, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * What a user types must be visible, measured in a real browser.
 *
 * ── The bug this exists for ──
 *
 * Every `<Input>` in the product set `text-base`, deliberately: iOS
 * Safari zooms the viewport for any field under 16px and strands the
 * user at that zoom. But the palette also had a colour token named
 * `base`, so Tailwind emitted `text-base` a SECOND time as
 * `color: #0B1026` — and the colour won.
 *
 * #0B1026 is the page background. Typed text was painted the exact
 * colour of the page behind it: a contrast ratio of 1:1, invisible.
 *
 * Nothing caught it. The class name reads correctly in the diff, `tsc`
 * sees a valid string, Tailwind emits both rules without complaint, and
 * the visual-regression baselines were captured WITH the bug, so they
 * agreed with it. A unit test cannot see it either — the collision only
 * exists once Tailwind has compiled both rules and a browser has
 * resolved the cascade.
 *
 * So this is an end-to-end computed-style assertion, which
 * `.claude/rules/frontend.md` names as the only gate that notices.
 *
 * ── Why it asserts a RATIO and not a hex value ──
 *
 * Pinning `rgb(242, 243, 248)` would fail the day somebody legitimately
 * warms the text token, and pass if the colour were changed to something
 * else equally unreadable. The property that matters is legibility.
 */

/**
 * A phone tail no other spec uses — see mask-letters.spec.ts.
 *
 * Phone rather than email because the email alphabet is exhausted: the
 * log masks an address to its first letter, all twenty-six are claimed,
 * and two specs sharing one read each other's OTP codes in parallel.
 */
const PHONE_TAIL = '745'

/** WCAG 2.1 contrast, over `rgb(r, g, b)` strings as the DOM reports them. */
function contrast(a: string, b: string): number {
  const luminance = (rgb: string): number => {
    const parts = rgb.match(/\d+(\.\d+)?/g)
    if (!parts || parts.length < 3) throw new Error(`unparsable colour: ${rgb}`)
    const [r, g, bl] = parts.slice(0, 3).map((n) => {
      const c = Number(n) / 255
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
    }) as [number, number, number]
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl
  }
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/** The colour actually painted behind an element, walking past transparency. */
async function effectiveBackground(page: Page, selector: string): Promise<string> {
  return page.evaluate((sel) => {
    let el: Element | null = document.querySelector(sel)
    while (el) {
      const bg = getComputedStyle(el).backgroundColor
      // rgba(…, 0) and `transparent` both mean "whatever is behind me".
      if (bg && !/,\s*0\s*\)$/.test(bg) && bg !== 'transparent') return bg
      el = el.parentElement
    }
    return getComputedStyle(document.body).backgroundColor
  }, selector)
}

test('typed text is legible against whatever is behind the field', async ({ page }) => {
  await page.goto('/auth')

  const field = page.getByLabel(/email address/i)
  await field.fill('Vatsalya')

  const colour = await field.evaluate((el) => getComputedStyle(el).color)
  const behind = await effectiveBackground(page, 'input[type="email"], #email')

  /*
    The precise assertion first: the text must not BE the background.
    This is the one that fails loudly on the exact defect, and its
    message names the cause rather than a ratio nobody can act on.
  */
  expect(
    colour,
    `the text a user types is rendered in ${colour}, which is the colour of ` +
      `the surface behind it. Almost certainly a Tailwind class collision — ` +
      `check that no colour token shares a name with a built-in utility ` +
      `(see apps/web/tailwind.config.ts and src/test/tailwind-tokens.test.ts).`,
  ).not.toBe(behind)

  const ratio = contrast(colour, behind)
  expect(
    ratio,
    `typed text is ${ratio.toFixed(2)}:1 against ${behind}, below the 4.5:1 ` +
      `WCAG AA floor for body text. A form field the user cannot read back ` +
      `is worse than one that rejects their input, because nothing signals ` +
      `that anything is wrong.`,
  ).toBeGreaterThanOrEqual(4.5)
})

test('the birth-place dropdown has a background of its own', async ({ page }) => {
  const phone = uniquePhone(PHONE_TAIL)
  const otp = watchOTP(phone)

  await page.goto('/auth')
  await page.getByRole('button', { name: /use phone instead/i }).click()
  await page.getByLabel(/phone number/i).fill(phone)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Vatsalya')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)

  await page.goto('/onboarding/birth')
  await page.getByLabel(/^day$/i).fill('20')
  await page.getByLabel(/^month$/i).fill('02')
  await page.getByLabel(/^year$/i).fill('2003')
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.getByLabel(/hour/i).fill('20')
  await page.getByLabel(/minute/i).fill('55')
  await page.getByRole('button', { name: /^continue$/i }).click()

  // 'jaip', like the other ten place-search tests, because that is what
  // tests/fixtures/places/cities-e2e.txt GUARANTEES. This line said
  // 'Prayagraj', which is not among the fixture's twenty cities — so it
  // passed on a developer machine whose places table still held a fuller
  // seed, and returned no options in CI. This test is about the
  // dropdown's BACKGROUND; which city produces the list is incidental.
  await page.getByLabel(/birth place/i).fill('jaip')
  const option = page.getByRole('option').first()
  await expect(option).toBeVisible()

  /*
    A floating list MUST paint its own background.

    It shipped as `bg-surface-2`, and there is no `surface-2` token.
    Tailwind drops an unknown colour class in silence, so the list was
    transparent and its suggestions were drawn over whatever happened to
    be underneath — legible only where the page behind them was plain.
  */
  const listBg = await option.evaluate((el) => {
    const list = el.closest('[role="listbox"]')
    return list ? getComputedStyle(list).backgroundColor : 'NO LISTBOX'
  })

  expect(
    /,\s*0\s*\)$/.test(listBg) || listBg === 'transparent',
    `the suggestions list is ${listBg} — it paints no background of its own, ` +
      `so results render over the page behind them. Most likely a colour ` +
      `class naming a token that does not exist; Tailwind drops those ` +
      `silently.`,
  ).toBe(false)

  // And the suggestion text itself must be readable on that background.
  const textColour = await option.evaluate(
    (el) => getComputedStyle(el.querySelector('span') as HTMLElement).color,
  )
  const ratio = contrast(textColour, listBg)
  expect(
    ratio,
    `a place name is ${ratio.toFixed(2)}:1 against the list behind it`,
  ).toBeGreaterThanOrEqual(4.5)
})

/**
 * The skip link, in the one state it is ever seen in.
 *
 * ── Why this was invisible to everything ──
 *
 * It is `sr-only` — one pixel, clipped — until it takes focus, at which
 * point it becomes a gold pill. axe scores `color-contrast` against the
 * element's CURRENT rendered state, so the failing state does not exist
 * during any scan. Three tests already focus this link
 * (`a11y.spec.ts`, `smoke.spec.ts`, `kundli-keyboard.spec.ts`) and all
 * three assert only that a focus ring appears and that Enter lands in
 * `<main>` — never a colour.
 *
 * It shipped broken. `focus:text-base` was doing two jobs: the font size,
 * and the navy that made the label readable on gold, back when the
 * palette had a colour token named `base`. Removing that token to fix
 * <Input> — whose text was being painted the page background by the same
 * collision — left this class emitting `font-size: 1rem` and no colour,
 * so the label inherited `text-ink`: #F2F3F8 on #D4A857, **1.99:1**
 * against a 4.5:1 floor, on every route.
 *
 * `input-contrast.spec.ts` was written for exactly that collision and
 * scoped to form fields, which is why it did not catch the second victim.
 * The lesson is the scope, not the technique: a colour regression is not
 * confined to the component that revealed it.
 */
test('the skip link is readable once it is visible', async ({ page }) => {
  await page.goto('/kundli/chart')

  await page.keyboard.press('Tab')
  await page.waitForTimeout(250)

  const link = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null
    if (!el) return null
    const cs = getComputedStyle(el)
    return {
      text: (el.textContent ?? '').trim(),
      color: cs.color,
      bg: cs.backgroundColor,
    }
  })

  expect(link?.text, 'the first tab stop is not the skip link').toMatch(/skip to content/i)

  /*
    The background must be opaque. If the gold pill stopped applying, the
    ratio below would be measured against the page and pass while the
    link rendered as unreadable overlapping text.
  */
  expect(
    /,\s*0\s*\)$/.test(link!.bg) || link!.bg === 'transparent',
    `the focused skip link has no background of its own (${link!.bg}), so it ` +
      `renders over whatever is beneath it`,
  ).toBe(false)

  const ratio = contrast(link!.color, link!.bg)
  expect(
    ratio,
    `the focused skip link is ${ratio.toFixed(2)}:1 (${link!.color} on ` +
      `${link!.bg}), below the 4.5:1 AA floor. This element exists for ` +
      `sighted keyboard users and it is the FIRST thing they reach on every ` +
      `route. Check that its foreground and background are named as a pair ` +
      `— a colour class that silently became a font size is how this broke ` +
      `the first time.`,
  ).toBeGreaterThanOrEqual(4.5)
})
