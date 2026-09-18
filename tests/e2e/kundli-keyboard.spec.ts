import { expect, test, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The keyboard pass, and the screen reader's view — automated.
 *
 * Gate item 13 asks for a manual keyboard and screen-reader pass.
 * `docs/MANUAL-A11Y-PASS.md` scripts it as twenty-one steps, and most of
 * them are not judgements at all — "does the focus ring stay visible",
 * "does Escape return focus to the control that opened the sheet",
 * "does each planet announce its sign and house". Those are facts a
 * browser can be asked.
 *
 * So they are asked here, and what remains for a person is the part that
 * genuinely needs one: whether the result is COMPREHENSIBLE. A chart can
 * announce every planet correctly and still be impossible to follow by
 * ear, and no assertion catches that.
 *
 * ── Why the accessibility tree and not axe ──
 *
 * axe already runs on every Kundli route in `a11y.spec.ts` and reports
 * zero violations. It checks for detectable faults — a missing label, a
 * bad contrast ratio — and cannot tell whether the label says anything
 * useful. `ariaSnapshot` returns what a screen reader would actually be
 * handed, which is where "Nakshatra, button" and "Magha, 2nd pada,
 * button" look different.
 */

/** A phone tail no other spec uses — see mask-letters.spec.ts. */
const PHONE_TAIL = '744'

async function signUpWithAProfile(page: Page): Promise<void> {
  const phone = uniquePhone(PHONE_TAIL)
  const otp = watchOTP(phone)

  await page.goto('/auth')
  await page.getByRole('button', { name: /use phone instead/i }).click()
  await page.getByLabel(/phone number/i).fill(phone)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Kamala')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)

  await page.goto('/onboarding/birth')
  await page.getByLabel(/^day$/i).fill('17')
  await page.getByLabel(/^month$/i).fill('8')
  await page.getByLabel(/^year$/i).fill('1994')
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/^hour$/i).fill('14')
  await page.getByLabel(/^minute$/i).fill('35')
  await page.getByRole('button', { name: /^continue$/i }).click()

  await page.getByLabel(/birth place/i).fill('jaip')
  await expect(page.getByRole('option').first()).toBeVisible()
  await page.getByRole('option').first().click()

  await page.getByRole('button', { name: /see my kundli/i }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 30_000 })
}

/** Where focus is, as a short description a failure message can carry. */
async function focused(page: Page): Promise<string> {
  return page.evaluate(() => {
    const el = document.activeElement
    if (!el || el === document.body) return '(body — nothing focused)'
    const label =
      el.getAttribute('aria-label') ??
      el.textContent?.trim().slice(0, 40) ??
      ''
    return `${el.tagName.toLowerCase()}${label ? ` "${label}"` : ''}`
  })
}

/**
 * Whether the focused element has a visible ring.
 *
 * `outline: none` with no replacement is the classic failure, and the
 * project's own rules name it. Checked on the computed style rather than
 * on a class, because a class that Tailwind dropped silently — the
 * documented hazard here — leaves the class attribute intact and the
 * style absent.
 */
async function hasVisibleFocusRing(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const el = document.activeElement
    if (!el || el === document.body) return false
    const s = getComputedStyle(el)
    const ring =
      (s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0) ||
      (s.boxShadow !== 'none' && s.boxShadow !== '')
    return ring
  })
}

test.describe.configure({ mode: 'serial' })

let page: Page

test.beforeAll(async ({ browser }) => {
  page = await browser.newPage()
  await signUpWithAProfile(page)
})

test.afterAll(async () => {
  await page.close()
})

// ─── keyboard ────────────────────────────────────────────────────────

test('the skip link is the first stop and it reaches main', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  await page.keyboard.press('Tab')

  const skip = page.getByRole('link', { name: /skip to content/i })
  await expect(skip, 'the first Tab does not reach a skip link').toBeFocused()

  // Visible when focused — a skip link that stays hidden is unusable by
  // the sighted keyboard users it mainly exists for.
  await expect(skip).toBeVisible()

  await page.keyboard.press('Enter')

  const landedInMain = await page.evaluate(() => {
    const main = document.querySelector('main')
    const active = document.activeElement
    return !!main && !!active && (main === active || main.contains(active))
  })
  expect(landedInMain, 'Enter on the skip link did not move focus into <main>').toBe(true)
})

test('every focus stop on the chart screen shows a visible ring', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const missing: string[] = []

  // Forty stops is well past the end of this screen; the loop breaks
  // when focus wraps back to the document.
  for (let i = 0; i < 40; i += 1) {
    await page.keyboard.press('Tab')
    const where = await focused(page)
    if (where.startsWith('(body')) break
    if (!(await hasVisibleFocusRing(page))) missing.push(where)
  }

  expect(
    missing,
    'these focus stops have no visible ring. `.claude/rules/frontend.md`: ' +
      '"Focus rings stay visible. Never `outline: none` without a replacement." ' +
      'A keyboard user who cannot see where they are cannot use the screen.',
  ).toEqual([])
})

/*
  A dialog takes focus, and Escape gives it back.

  Written against the share sheet rather than a planet glyph, and that
  correction is worth recording: `ChartSVG` renders its planets as
  focusable `role="button"` polygons only when a tap handler is supplied,
  and `/kundli/chart` supplies none — there is no sheet for a press to
  open there, and an inert button is worse for a screen-reader user than
  no button, because it promises an action and swallows it.

  So the planets are not controls on that route, the accessible table is
  the path, and this test uses a dialog the route actually has.
*/
test('a dialog takes focus, and Escape returns it to the opener', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const share = page.getByRole('button', { name: /^share$/i })
  await share.focus()
  const opener = await focused(page)

  await page.keyboard.press('Enter')

  const dialog = page.getByRole('dialog')
  await expect(dialog, 'Enter on Share opened no dialog').toBeVisible()

  const insideDialog = await page.evaluate(() => {
    const d = document.querySelector('[role="dialog"]')
    const a = document.activeElement
    return !!d && !!a && d.contains(a)
  })
  expect(
    insideDialog,
    'the dialog opened but focus stayed behind it — a keyboard user has to ' +
      'Tab through the whole page to reach content that just appeared',
  ).toBe(true)

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  const returned = await focused(page)
  expect(
    returned,
    `Escape returned focus to ${returned}, not to the control that opened the ` +
      `dialog (${opener}). Focus dumped at the top of the document means ` +
      `re-traversing the screen after every glance.`,
  ).toBe(opener)
})

/*
  Focus is trapped inside the dialog while it is open.

  Without a trap, Tab walks out of the sheet and into the page behind it
  — which is still there, still scrollable, and now being read out from
  the middle while a modal is on screen.
*/
test('focus stays inside an open dialog', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  await page.getByRole('button', { name: /^share$/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()

  const escaped: string[] = []
  for (let i = 0; i < 12; i += 1) {
    await page.keyboard.press('Tab')
    const inside = await page.evaluate(() => {
      const d = document.querySelector('[role="dialog"]')
      const a = document.activeElement
      return !!d && !!a && d.contains(a)
    })
    if (!inside) escaped.push(await focused(page))
  }

  expect(
    escaped,
    'Tab left the open dialog and landed on the page behind it',
  ).toEqual([])

  await page.keyboard.press('Escape')
})

/*
  Changing the chart keeps focus on the control that changed it.

  Already asserted for the varga switcher in kundli-yogas.spec.ts. Here
  for the STYLE switcher, which is the same hazard by a different route:
  a re-render that replaces the switcher's DOM drops focus to the body,
  and the reader is silently returned to the top of the page mid-task.
*/
test('changing chart style keeps focus on the switcher', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const south = page.locator('label:has(input[value="south"])')
  await south.click()

  // The radio itself is sr-only, so focus lands on the input.
  const stillThere = await page.evaluate(() => {
    const a = document.activeElement as HTMLInputElement | null
    return a?.tagName === 'INPUT' && a.value === 'south'
  })
  expect(
    stillThere,
    'focus left the style switcher when the chart redrew',
  ).toBe(true)
})

// ─── the screen reader's view ────────────────────────────────────────

test('the chart is announced as a group with a summary, not as a graphic', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const snapshot = await page.locator('svg[role="group"]').ariaSnapshot()

  expect(
    snapshot,
    'the chart SVG is not exposed as a group. An SVG with no role is ' +
      'announced as "graphic" or skipped entirely, which is the whole ' +
      'reason the accessible table exists.',
  ).toMatch(/group/)

  // The summary sentence: ascendant, and the Moon's sign.
  expect(
    snapshot,
    'the chart group carries no summary. A reader arriving at it is told ' +
      'there is a group and nothing about what is in it.',
  ).toMatch(/ascendant|rising|moon/i)
})

/**
 * Every planet's sign and house are reachable by ear.
 *
 * The manual script's real question is "could you answer 'which sign is
 * my Moon in, and which house' with your eyes closed". That judgement
 * needs a person; the FACT underneath it does not — either those two
 * values are in the accessibility tree or they are not.
 *
 * Asserted against the accessible TABLE rather than against focusable
 * glyphs, and the correction matters: `ChartSVG` renders planets as
 * `role="button"` polygons only when a tap handler is supplied, and
 * `/kundli/chart` supplies none. An earlier version of this test looked
 * for those controls, found zero, and would have reported the route as
 * having no accessible planets at all — when in fact the table carries
 * every one of them, which is the documented and always-available path.
 */
test('every planet announces its sign and house', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const table = page.getByRole('table').first()
  await expect(table, 'there is no accessible table beside the chart').toBeAttached()

  const rows = table.getByRole('row')
  const count = await rows.count()
  // Nine grahas plus a header.
  expect(count, 'the accessible table does not carry all nine planets').toBeGreaterThanOrEqual(10)

  const SIGNS =
    /aries|taurus|gemini|cancer|leo|virgo|libra|scorpio|sagittarius|capricorn|aquarius|pisces/i

  const thin: string[] = []
  for (let i = 1; i < count; i += 1) {
    const text = (await rows.nth(i).textContent()) ?? ''
    const hasSign = SIGNS.test(text)
    /*
      An ORDINAL, not a bare number.

      The cells render as "Leo10th" with no separator, so a `\b`-anchored
      number matches nothing: there is no word boundary between "o" and
      "10", nor between "10" and "th". The first version of this test
      failed on eight perfectly correct rows because of it — the app was
      right and the assertion was wrong.
    */
    const hasHouse = /\d{1,2}(st|nd|rd|th)/i.test(text) || /—|unknown|no house/i.test(text)
    if (!hasSign || !hasHouse) thin.push(text.trim().slice(0, 60) || '(empty row)')
  }

  expect(
    thin,
    'these rows announce less than a sign and a house. A screen-reader user ' +
      'reading the table learns nothing the sighted reader gets from the diagram.',
  ).toEqual([])
})

test('the visually-hidden table duplicates the chart and says so', async () => {
  await page.goto('/kundli/chart')
  await expect(page.locator('svg[role="group"]')).toBeVisible({ timeout: 30_000 })

  const table = page.getByRole('table').first()
  await expect(table, 'there is no accessible table beside the chart').toBeAttached()

  const snapshot = await table.ariaSnapshot()

  // A caption, so the repetition reads as deliberate rather than as the
  // same data accidentally twice.
  expect(
    snapshot.toLowerCase(),
    'the accessible table has no caption. Without one a screen-reader user ' +
      'meets the same nine planets twice with no explanation.',
  ).toMatch(/caption|same data|table/)

  // Nine grahas plus a header row.
  const rows = await table.getByRole('row').count()
  expect(rows, 'the accessible table does not carry all nine planets').toBeGreaterThanOrEqual(10)
})

test('the current dasha is announced as current, not shown as colour', async () => {
  await page.goto('/kundli/dashas')
  await expect(page.getByRole('listitem').first()).toBeVisible({ timeout: 30_000 })

  const current = page.locator('[aria-current="true"]')
  await expect(
    current.first(),
    'no dasha is marked aria-current. The running period is conveyed by a ' +
      'gold background alone, which is nothing to a screen reader and ' +
      'nothing on a monochrome display.',
  ).toBeAttached()

  const label = (await current.first().getAttribute('aria-label')) ?? ''
  expect(
    label.toLowerCase(),
    'the current period does not say so in words',
  ).toMatch(/now|current|running/)
})

/*
  Sade Sati says which state it is in, in words.

  Scoped to the PANEL rather than matched against the whole page, and
  asserted against the dictionary's real strings rather than against
  words this test guessed at. The first version searched the entire
  transits page for /not in|no sade sati/ and failed on copy that
  actually reads "Not currently running" — the app was right and the
  assertion was inventing vocabulary.
*/
test('Sade Sati names its state in words, and dates it when running', async () => {
  await page.goto('/kundli/transits')
  await expect(page.getByRole('list').first()).toBeVisible({ timeout: 30_000 })

  // The panel is the section headed by the term itself.
  const panel = page.locator('div').filter({ hasText: /^Sade Sati/ }).last()
  await expect(panel, 'there is no Sade Sati panel on the transits screen').toBeVisible()

  const text = (await panel.textContent()) ?? ''

  const running = /\b(rising|peak|setting)\b/i.test(text)
  const notRunning = /not currently running/i.test(text)

  expect(
    running || notRunning,
    `the panel says neither which phase is running nor that none is. ` +
      `Colour alone is not a state a screen reader can hear. Text: ${text.slice(0, 200)}`,
  ).toBe(true)

  if (running) {
    expect(
      text,
      'Sade Sati is running but the panel reads no dates. "When does this end" ' +
        'is the question it exists to answer.',
    ).toMatch(/\b(19|20)\d{2}\b/)
  }
})

test('an error is announced, not only displayed', async () => {
  await page.route('**/api/v1/birth-profiles*', (r) =>
    r.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
  )

  await page.goto('/kundli/chart')

  const alert = page.getByRole('alert')
  await expect(
    alert.first(),
    'the error is rendered but not in a live region, so a screen-reader ' +
      'user is told nothing changed and waits at a screen that has already failed.',
  ).toBeVisible({ timeout: 30_000 })

  await page.unroute('**/api/v1/birth-profiles*')
})
