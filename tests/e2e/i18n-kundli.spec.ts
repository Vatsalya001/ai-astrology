import { devices, expect, test, type BrowserContext, type Page } from '@playwright/test'

import { MOON_DAYS, MOON_SIGNS, YOGAS, YOGA_KEYS } from '@ayana/content'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * The Kundli screens in Hindi.
 *
 * `hardcoded-strings.test.ts` proves no component holds an English
 * literal, and `dictionaries.test.ts` proves every key exists in both
 * locales. Neither proves the screens actually RENDER the other one — a
 * component can pull every string from `t.chart.*` and still be wired to
 * a provider that never changes, and both of those suites stay green.
 *
 * This is the half that cannot be checked without a browser.
 */
test.use({ ...devices['Pixel 7'], viewport: { width: 360, height: 780 } })
test.describe.configure({ mode: 'serial' })

let shared: Page
let context: BrowserContext

/** Any Devanagari character. */
const DEVANAGARI = /[ऀ-ॿ]/

test.beforeAll(async ({ browser }) => {
  context = await browser.newContext({
    ...devices['Pixel 7'],
    viewport: { width: 360, height: 780 },
  })
  shared = await context.newPage()
  await signUp(shared)
  await addBirthProfile(shared)
})

test.afterAll(async () => {
  await context.close()
})

async function signUp(page: Page): Promise<void> {
  const phone = uniquePhone('905')

  await page.goto('/auth')
  await page.getByRole('button', { name: /use phone instead/i }).click()

  const otp = watchOTP(phone)
  await page.getByLabel(/phone number/i).fill(phone)
  await page.getByRole('button', { name: /^continue$/i }).click()
  await page.locator('input[autocomplete="one-time-code"]').fill(await otp.next())

  await expect(page).toHaveURL(/\/onboarding\/name/)
  await page.getByLabel(/your name/i).fill('Priya')
  await page.getByRole('button', { name: /finish/i }).click()
  await expect(page).toHaveURL(/\/home$/)
}

async function addBirthProfile(page: Page): Promise<void> {
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

/** Sets the stored locale the way the picker does, then reloads. */
async function useLocale(page: Page, locale: 'en' | 'hi'): Promise<void> {
  await page.evaluate((l) => window.localStorage.setItem('ayana.locale', l), locale)
  await page.reload()
}

test.describe('switching to Hindi', () => {
  /**
   * The real picker, once.
   *
   * Every other test here sets localStorage directly, which is faster
   * and less brittle. Doing it through the settings screen at least once
   * proves the path a user actually takes still works — otherwise the
   * whole file could pass against a picker that writes the wrong key.
   */
  test('the language picker changes the stored locale', async () => {
    const page = shared
    await page.goto('/settings/preferences')

    /*
      The label, not the input. The preferences radios are `sr-only` —
      one pixel, clipped — so Playwright refuses to click them, and
      `force: true` would paper over a control that genuinely could not
      be clicked. Clicking the label is what a user does. Same pattern
      as the varga switcher.
    */
    await page.locator('label:has(input[value="hi"])').click()

    await expect
      .poll(() => page.evaluate(() => window.localStorage.getItem('ayana.locale')))
      .toBe('hi')

    // And put it back, so the ordering of the tests below is theirs.
    await page.locator('label:has(input[value="en"])').click()
  })

  test('the planets screen renders Hindi', async () => {
    const page = shared

    await useLocale(page, 'en')
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const english = (await page.locator('main').textContent()) ?? ''
    expect(english).toContain('Planets')
    expect(english, 'English render already contains Devanagari').not.toMatch(DEVANAGARI)

    await useLocale(page, 'hi')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const hindi = (await page.locator('main').textContent()) ?? ''

    /*
      The heading, the column headers and the section labels all have to
      move. Asserting only "some Devanagari appears" would pass on a
      screen where one word translated and the rest did not.
    */
    expect(hindi).toContain('ग्रह और भाव') // heading
    expect(hindi).toContain('राशि') // Sign column
    expect(hindi).toContain('अंश') // Degree column
    expect(hindi).toContain('भाव') // House
    expect(hindi, 'the English heading survived the switch').not.toContain('Planets & houses')
  })

  test('the dasha screen renders Hindi', async () => {
    const page = shared
    await useLocale(page, 'hi')
    await page.goto('/kundli/dashas')
    await expect(page.getByRole('listitem')).toHaveCount(9, { timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''
    expect(text).toContain('दशा काल')
    expect(text).not.toContain('Dasha periods')
  })

  test('the transits screen renders Hindi, including Sade Sati', async () => {
    const page = shared
    await useLocale(page, 'hi')
    await page.goto('/kundli/transits')
    await expect(page.getByRole('list', { name: /गोचर/ })).toBeVisible({ timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''
    expect(text).toContain('इस समय आकाश में')
    expect(text).toContain('साढ़े साती')
    expect(text).not.toContain('Right now in the sky')
  })

  /**
   * Two different sources have to switch on this screen, and only one of
   * them is the dictionary.
   *
   * The heading and the strength word come from `t.chart.*`. The yoga
   * NAMES and descriptions come from `@ayana/content` via
   * `describeYoga(name, locale)` — a separate corpus with its own
   * lookup, and the one that is easy to leave pinned to `'en'`.
   *
   * The first version of this test asserted only `योग`, `मध्यम` and
   * "some Devanagari appears", every one of which is satisfied by the
   * DICTIONARY alone. Pinning `describeYoga` to English left it green,
   * while its own failure message claimed to be checking the corpus.
   * So it now asserts on the corpus directly, in both directions.
   */
  test('the yogas screen renders Hindi from both the dictionary and the corpus', async () => {
    const page = shared
    await useLocale(page, 'hi')
    await page.goto('/kundli/yogas')
    await expect(page.getByRole('button').first()).toBeVisible({ timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''

    // The dictionary half.
    expect(text).toContain('योग')
    expect(text).toContain('मध्यम')
    expect(text).not.toContain('Moderate')

    // The corpus half: no yoga may render its ENGLISH name, whichever
    // ones this chart happens to have.
    const englishNames = YOGA_KEYS.map((k) => YOGAS[k].en.name).filter((n) => text.includes(n))
    expect(
      englishNames,
      'describeYoga is still answering in English while the dictionary is in Hindi',
    ).toEqual([])

    // And at least one Hindi yoga name IS present, so the assertion
    // above cannot pass by the cards having failed to render at all.
    const hindiNames = YOGA_KEYS.map((k) => YOGAS[k].hi.name).filter((n) => text.includes(n))
    expect(hindiNames.length, 'no yoga rendered at all').toBeGreaterThan(0)
  })

  test('the dashboard renders Hindi', async () => {
    const page = shared
    await useLocale(page, 'hi')
    await page.goto('/home')
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible({ timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''

    // The dictionary half.
    expect(text).toContain('आज') // Today
    expect(text).toContain('अपने AI ज्योतिषी से पूछें') // Ask box
    expect(text).not.toContain('Ask your AI astrologer')

    /*
      The corpus half. The Today line comes from `moonDay(sign, locale)`,
      not from the dictionary, and which of the twelve shows depends on
      where the Moon is today — so this checks that NONE of the English
      lines appears and that one Hindi line does.

      Asserting only "some Devanagari appears" would be satisfied by the
      heading above, which is the mistake the yoga test made.
    */
    const englishLines = MOON_SIGNS.map((s) => MOON_DAYS[s].en.short).filter((l) =>
      text.includes(l),
    )
    expect(
      englishLines,
      'the moon-day corpus is still answering in English',
    ).toEqual([])

    const hindiLines = MOON_SIGNS.map((s) => MOON_DAYS[s].hi.short).filter((l) =>
      text.includes(l),
    )
    expect(hindiLines.length, 'no moon-day line rendered at all').toBe(1)
  })

  test('switches back to English cleanly', async () => {
    const page = shared
    await useLocale(page, 'en')
    await page.goto('/kundli/planets')
    await expect(page.getByRole('row')).toHaveCount(10, { timeout: 30_000 })

    const text = (await page.locator('main').textContent()) ?? ''
    expect(text).toContain('Planets & houses')
    expect(text, 'Hindi copy survived the switch back').not.toMatch(DEVANAGARI)
  })
})
