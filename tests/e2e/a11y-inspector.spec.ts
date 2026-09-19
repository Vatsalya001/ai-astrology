import { expect, test, type Locator, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * Jump via the panel's selector.
 *
 * The four jump buttons became a labelled `<select>` because a tester
 * asked to find the planet table did not recognise "The data table" as a
 * destination. Driving it by OPTION VALUE rather than by visible label
 * keeps these tests stable while the labels are reworded — and the
 * labels are the part most likely to change, since their whole job is to
 * be recognisable.
 */
async function jumpTo(panel: Locator, target: 'start' | 'main' | 'chart' | 'table') {
  await panel.locator('select#a11y-jump').selectOption(target)
}

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

  // A jump target, so the tester never has to "click on empty dark
  // space" to put focus in the page.
  await jumpTo(panel, 'main')

  expect((await panel.textContent()) ?? '', 'the panel shows no focused element').toContain(
    'Focused now',
  )

  /*
    Tab until the email field is reached, rather than once.

    ── This test used to pass for the wrong reason ──

    It pressed Tab exactly once from `main` and asserted the whole panel
    contained "Email address". It did — but not because focus had reached
    the input. `accessibleName` fell back to `textContent` for every
    element, so focusing `main` reported main's ENTIRE flattened text as
    its name, and the label was in there. The assertion was reading a bug.

    One Tab from `main` lands on the "Ayana home" link, which sits inside
    main and before the form. So the thing this test claims to check —
    that a `<label for>` is resolved into an accessible name — was never
    being exercised at all.

    Walking until the field is focused tests it for real, and the bound
    keeps a runaway loop from looking like a pass.
  */
  let focusedName = ''
  for (let press = 0; press < 6; press++) {
    await page.keyboard.press('Tab')
    await page.waitForTimeout(150)
    focusedName = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null
      return el?.tagName.toLowerCase() === 'input' ? 'INPUT' : (el?.textContent ?? '').trim()
    })
    if (focusedName === 'INPUT') break
  }

  expect(focusedName, 'never reached a form field within six tab presses').toBe('INPUT')

  /*
    The email field's name comes from its <label>, which is the case
    `accessibleName` is most likely to get wrong: aria-label and text
    content are easy; a label element's `for` needs a lookup.
  */
  const focusedBlock = panel.locator('text=Focused now').locator('..')
  expect(
    (await focusedBlock.textContent()) ?? '',
    "the panel did not resolve the email input's label — it reads names " +
      'from aria-label, aria-labelledby, <label for>, then text content, ' +
      'and this case exercises the third',
  ).toMatch(/Email address/i)
})

test('it does not count itself in the focus order', async ({ page }) => {
  await page.goto('/auth?a11y=1')
  const panel = page.locator('[data-a11y-inspector]')
  await expect(panel).toBeVisible({ timeout: 15_000 })

  // Use two of the panel's OWN controls. Neither is part of the page
  // under test, so neither may appear in the log.
  await jumpTo(panel, 'main')
  await panel.getByRole('button', { name: 'Reset' }).click()
  await page.waitForTimeout(300)

  const text = (await panel.textContent()) ?? ''
  // Heading format: 'Focus order — N tab stops'. Jumps are shown but not
  // counted, so clicking the panel's own buttons must leave this at 0.
  const count = Number((text.match(/Focus order — (\d+) tab stop/) ?? [])[1] ?? -1)

  expect(
    count,
    'the panel logged its own controls as focus stops. "How many tabs to ' +
      'reach the chart" is the number this tool exists to report, and it ' +
      'is wrong the moment the tool counts itself.',
  ).toBe(0)
})

/** A phone tail no other spec uses — see mask-letters.spec.ts. */
const PHONE_TAIL = '748'

async function signUpWithAChart(page: Page): Promise<void> {
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

/**
 * The tool gave a FALSE NEGATIVE on correct markup, and that is the only
 * kind of bug an accessibility checker must never have.
 *
 * Asked "which sign is your Moon in, and which house?", a tester clicked
 * **The data table** and the panel reported the focused element's name as
 *
 *   "Planetary positions — …as a table.PlanetSignHouseDegreeNakshatra
 *    NotesAscendantVirgo1st2"
 *
 * — the caption with every cell of the first two rows run together and
 * then cut off at 120 characters, before the Moon. They could not answer,
 * and would reasonably have concluded the table was unreadable.
 *
 * It is not. It has a `<caption>`, six `scope="col"` headers and a `<th>`
 * per row; a screen reader on that markup says "Moon, House, 4th". Two
 * separate defects in the TOOL produced that:
 *
 *   1. `accessibleName` fell back to `textContent` for every element.
 *      That is right for a button, whose content is its name, and wrong
 *      for a container, which a screen reader names and then lets you
 *      navigate INTO.
 *   2. There was no way to navigate into it. A panel that reports a
 *      table's name and not its rows cannot answer a question whose
 *      answer is in a cell.
 *
 * The same session also logged nineteen "focus stops" on a page with
 * eight, because every click of a jump button counted as a Tab press —
 * which makes "how many tabs to reach the Moon", the one number this
 * tool exists to report, meaningless.
 */
test.describe('reading a table', () => {
  /*
    Serial, with one signup shared.

    Both tests need a chart, and two signups running concurrently on the
    same phone tail read each other's OTP — the mask keeps three leading
    characters and three trailing digits, so within one tail two
    concurrent numbers are indistinguishable. `mask-letters.spec.ts`
    guards tails ACROSS files; inside one file, serialising is the fix.
  */
  test.describe.configure({ mode: 'serial' })

  let chartPage: Page

  test.beforeAll(async ({ browser }) => {
    chartPage = await browser.newPage()
    await signUpWithAChart(chartPage)
  })

  test.afterAll(async () => {
    await chartPage.close()
  })

  test('names the table by its caption, not its contents', async () => {
    const page = chartPage
    await page.goto('/kundli/chart?a11y=1')

    const panel = page.locator('[data-a11y-inspector]')
    await expect(panel).toBeVisible({ timeout: 15_000 })
    await jumpTo(panel, 'table')

    const focusedBlock = panel.locator('text=Focused now').locator('..')
    const name = (await focusedBlock.textContent()) ?? ''

    expect(
      name,
      'the table is named by its caption; a run-together wall of cell text ' +
        'is what a screen reader would NOT say, and reporting it made ' +
        'correct markup look broken',
    ).toContain('Planetary positions')

    expect(
      name,
      'the accessible name still contains flattened cell text — ' +
        '`accessibleName` is falling back to textContent for a container',
    ).not.toMatch(/PlanetSignHouse|SignHouseDegree/)
  })

  test('reads the Moon row well enough to answer sign and house', async () => {
    const page = chartPage
    await page.goto('/kundli/chart?a11y=1')

    const panel = page.locator('[data-a11y-inspector]')
    await expect(panel).toBeVisible({ timeout: 15_000 })
    await jumpTo(panel, 'table')

    const text = (await panel.textContent()) ?? ''

    /*
      The question the Phase 3 manual pass actually asks. Not pinned to a
      particular sign — the fixture may change — but the row must carry
      the Moon, a sign, and a house, each attached to its header.
    */
    expect(
      text,
      'the panel does not read the table, so the question it exists to ' +
        'answer — "which sign is my Moon in, and which house?" — cannot be ' +
        'answered from it',
    ).toMatch(/Moon,\s*Sign:\s*\w+,\s*House:\s*\d+\w+/)
  })

  /**
   * Q2 of the manual pass — "listen to the whole page top to bottom" —
   * is about READING order, not focus order.
   *
   * The panel could only report the eight things you can Tab to on this
   * route. Reading order is every heading, paragraph, table cell and
   * visually-hidden string, in document order, and a tester handed a list
   * of eight buttons cannot judge whether the page "tells a story or
   * jumps around". They would have had to install a screen reader for the
   * one question this tool exists to spare them.
   */
  test('reads the whole page in document order, not just the focusable parts', async () => {
    const page = chartPage
    await page.goto('/kundli/chart?a11y=1')
    const panel = page.locator('[data-a11y-inspector]')
    await expect(panel).toBeVisible({ timeout: 15_000 })

    await panel.getByRole('button', { name: /Read the whole page aloud/i }).click()
    await page.waitForTimeout(400)

    const lines = await panel.locator('ol li').allTextContents()

    expect(lines.length, 'the reading produced almost nothing').toBeGreaterThan(15)

    // Structure a reader navigates by, in the order it appears.
    const markers = lines.filter((l) => l.startsWith('▸'))
    expect(markers.some((l) => /heading 1/.test(l)), 'no h1 in the reading').toBe(true)
    expect(markers.some((l) => /^▸ table/.test(l)), 'the data table is not announced as a table').toBe(true)

    /*
      The regression this pins.

      The first version returned as soon as it met a `sr-only` element,
      so the chart's visually-hidden data table — a sr-only <div> wrapping
      a captioned <table> — came out as ONE 600-character line with every
      cell run together. That is the same "a container is not its text"
      mistake this file already fixed once in `accessibleName`, made again
      twelve functions later.
    */
    const longest = Math.max(...lines.map((l) => l.length))
    expect(
      longest,
      `a single line is ${longest} characters — the hidden table is being ` +
        `flattened into a blob instead of read row by row. A wrapper is not ` +
        `a leaf just because it is invisible.`,
    ).toBeLessThan(200)

    // And the data itself must survive, row-addressable.
    expect(
      lines.some((l) => /Moon,\s*Sign:\s*\w+,\s*House:\s*\d+\w+/.test(l)),
      'the Moon row is not readable from the page reading',
    ).toBe(true)
  })

  test('a jump button is not counted as a tab stop', async ({ page }) => {
    await page.goto('/auth?a11y=1')
    const panel = page.locator('[data-a11y-inspector]')
    await expect(panel).toBeVisible({ timeout: 15_000 })

    // Three jumps, no Tab presses.
    await jumpTo(panel, 'main')
    await jumpTo(panel, 'start')
    await jumpTo(panel, 'main')

    expect(
      (await panel.textContent()) ?? '',
      'clicking a jump button incremented the tab-stop count. "How many ' +
        'tabs to reach X" is the number this tool exists to report, and it ' +
        'is wrong the moment a click counts as a keystroke.',
    ).toContain('Focus order — 0 tab stops')

    await page.keyboard.press('Tab')
    await page.waitForTimeout(300)
    expect(await panel.textContent()).toContain('Focus order — 1 tab stop')
  })
})

/**
 * Tabbing past the last control does not invent new stops.
 *
 * Press Tab on the final focusable element and focus leaves for the
 * browser's own chrome; press it again and Chrome hands focus back to
 * that same element, firing `focusin` a second time. The log showed
 * "Download PDF" as stops 3, 4 AND 5 — on a page whose entire tab order
 * is eight stops, exactly one of which is Download PDF.
 *
 * Read literally that says there are three download buttons. It sent a
 * tester hunting a duplicate-control bug that does not exist, which is
 * the same class of failure as the table blob: the tool inventing a
 * product defect out of its own bookkeeping.
 */
test('focus returning to the same element is not a second tab stop', async ({ page }) => {
  await page.goto('/auth?a11y=1')
  const panel = page.locator('[data-a11y-inspector]')
  await expect(panel).toBeVisible({ timeout: 15_000 })

  /*
    Reproduced by blur-then-refocus, NOT by pressing Tab past the end.

    The first version of this test pressed Tab fourteen times and asserted
    the count stayed low. It passed with the guard REMOVED, so it was
    testing nothing: headless Chromium has no browser chrome to hand focus
    to, so Tab wraps inside the page and the same element is never
    re-focused back-to-back. The behaviour only occurs in a real browser.

    `blur()` sends focus to the document body — the same place it goes
    when it leaves for the address bar — and `focus()` brings it back to
    the element it just left, firing `focusin` a second time. That is the
    exact sequence, and it reproduces headlessly.
  */
  await page.keyboard.press('Tab')
  await page.waitForTimeout(150)

  await page.evaluate(() => {
    const el = document.activeElement as HTMLElement
    el.blur()
    el.focus()
    el.blur()
    el.focus()
  })
  await page.waitForTimeout(250)

  const text = (await panel.textContent()) ?? ''
  const stops = Number((text.match(/Focus order — (\d+) tab stop/) ?? [])[1] ?? -1)

  expect(
    stops,
    `one Tab press plus two re-focuses of that SAME element produced ` +
      `${stops} tab stops. Focus re-entering the element it just left is ` +
      `being counted as a new stop, which reads as duplicate controls that ` +
      `do not exist — a tester saw "Download PDF" logged as stops 3, 4 and ` +
      `5 on a page with exactly one download button.`,
  ).toBe(1)

  // And no control may appear twice in a row in the log.
  const names = [...text.matchAll(/(\d+)\. ([^(]+)\(/g)].map((m) => m[2]!.trim())
  const backToBack = names.filter((n, i) => i > 0 && n === names[i - 1])
  expect(
    backToBack,
    'the same control is logged as two consecutive tab stops',
  ).toEqual([])
})
