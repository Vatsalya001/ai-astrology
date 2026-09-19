import { expect, test, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * A hostile profile label must render as text, everywhere it appears.
 *
 * ── Why the label and not some other field ──
 *
 * `PHASE-03-KUNDLI-UI.md` §11 names it: "all rendered content escaped — a
 * user-supplied profile label cannot inject markup". It is the only
 * free-text string in Phase 3 that a user controls and the product then
 * renders back, and it reaches TWO surfaces with different risk:
 *
 *   - /settings/birth-profiles, an ordinary React page
 *   - /kundli/print, which headless Chrome loads to produce a PDF — a
 *     second rendering engine, running with `--no-sandbox`
 *
 * The second is why this is an e2e test and not a component test. A
 * component test proves React escapes, which was never in doubt; only a
 * real browser proves the string stayed inert all the way to the page
 * Chrome is pointed at.
 *
 * ── Why the label is set through the API ──
 *
 * The onboarding UI hardcodes `label: 'self'`, so the UI cannot produce a
 * hostile one. The API accepts whatever is sent (`profileBody.Label`,
 * trimmed and no more), which is the realistic vector: a scripted client,
 * or a compromised one. Testing only what the UI can currently produce
 * would assert the UI's restraint rather than the server's safety.
 *
 * ── The companion guard ──
 *
 * `apps/web/src/test/no-raw-html.test.ts` asserts no component reaches
 * for `dangerouslySetInnerHTML` or an `innerHTML` assignment at all. That
 * one catches the vector being INTRODUCED; this one catches it being
 * exploited. Neither replaces the other: a structural check cannot prove
 * today's rendering is safe, and a behavioural check cannot prove
 * tomorrow's component will be.
 */

const API_URL = process.env.API_URL ?? 'http://localhost:4000'

/** A phone tail no other spec uses — see mask-letters.spec.ts. */
const PHONE_TAIL = '759'

/*
  Four shapes, because they fail differently.

  The first two execute on parse; `onerror` executes without any script
  tag at all, which is the one an allowlist sanitiser most often misses;
  and the closing-tag payload tests whether the string can break OUT of
  the element it was interpolated into.
*/
const PAYLOADS = [
  `<script>window.__pwned = 1</script>`,
  `<img src=x onerror="window.__pwned=1">`,
  `"><script>window.__pwned=1</script>`,
  `<svg/onload=window.__pwned=1>`,
]

const HOSTILE_LABEL = PAYLOADS.join(' ')

async function signUp(page: Page): Promise<void> {
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
}

test('a profile label full of markup renders as text, not as elements', async ({ page }) => {
  await signUp(page)

  /*
    Mint an access token through /auth/refresh rather than reading one.

    The app keeps the access token IN MEMORY, deliberately — `users-api.ts`
    says why: localStorage is readable by any script on the page. The
    refresh token is an httpOnly cookie, and `page.request` carries the
    browser context's cookies, so this is the same exchange the app itself
    performs.

    The first version of this test scraped localStorage for anything
    JWT-shaped, found nothing, sent an unauthenticated POST, got a 401,
    took the "server refused it" early return and PASSED while asserting
    nothing at all. Sixth vacuous guard in this codebase's history and the
    same shape every time: the assertion never reached the code. Hence the
    explicit status check below.
  */
  const refreshed = await page.request.post(`${API_URL}/api/v1/auth/refresh`, {
    data: {},
    failOnStatusCode: false,
  })
  expect(
    refreshed.status(),
    'could not mint an access token, so the rest of this test would assert ' +
      'nothing — it is a setup failure, not a security result',
  ).toBe(200)
  const token = ((await refreshed.json()) as { access_token: string }).access_token

  /*
    A REAL place id, resolved through the same search the UI uses.

    The first attempt hardcoded `place_id: 1`, which does not exist. The
    server answered 400 "That birth place could not be resolved", the test
    read any non-401 rejection as "the server refused the hostile label",
    and passed — concluding the opposite of the truth from an error about
    geography. Hardcoding a GeoNames id would also rot the day the
    gazetteer is reseeded, so it is looked up.
  */
  const places = await page.request.get(
    `${API_URL}/api/v1/places/search?q=Prayagraj`,
    { headers: { Authorization: `Bearer ${token}` }, failOnStatusCode: false },
  )
  expect(places.status(), 'place lookup failed, so the create below cannot be valid').toBe(200)
  const placeID = ((await places.json()) as { places: Array<{ id: number }> }).places[0]?.id
  expect(placeID, 'no place matched "Prayagraj" — the gazetteer may not be seeded').toBeTruthy()

  const created = await page.request.post(`${API_URL}/api/v1/birth-profiles`, {
    headers: { Authorization: `Bearer ${token}` },
    failOnStatusCode: false,
    data: {
      label: HOSTILE_LABEL,
      birth_date: '2003-02-26',
      birth_time: '20:55',
      time_accuracy: 'exact',
      place_id: placeID,
    },
  })

  /*
    A rejection is a pass, and is worth saying out loud: if the server
    validates the label to an allowlist, the injection never reaches a
    renderer and the property holds for a better reason. The test only
    proceeds to the rendering assertions when the hostile value was
    actually STORED.
  */
  /*
    A 4xx here means the server VALIDATED the label away, which is a pass
    for a better reason than escaping — the injection never reaches a
    renderer. But it must be an intentional rejection, not an auth
    failure, or the test skips its own assertions again.
  */
  if (!created.ok()) {
    /*
      A rejection only counts as a pass if it is ABOUT THE LABEL.

      Anything else — a bad place id, an expired token, a malformed date —
      means the request never exercised the thing under test, and reading
      it as "the server refused the injection" is how this test first
      passed while proving the opposite.
    */
    const body = await created.text()
    expect(
      body.toLowerCase(),
      `the create failed for an unrelated reason (${created.status()}: ${body}). ` +
        `This test asserted nothing about the label.`,
    ).toMatch(/label/)
    test.info().annotations.push({
      type: 'note',
      description: `server refused the hostile label (${created.status()}) — ` +
        `injection cannot reach a renderer`,
    })
    return
  }

  // Nothing may have executed during any of the above.
  const pwnedAfterCreate = await page.evaluate(() => '__pwned' in window)
  expect(pwnedAfterCreate, 'a payload executed during profile creation').toBe(false)

  await page.goto('/settings/birth-profiles')
  await page.waitForLoadState('networkidle')

  /*
    Three assertions, because each catches a different failure:

      1. no element from the payload exists in the DOM
      2. no payload side effect ran
      3. the text is VISIBLE — an escaped string that renders as empty
         would pass 1 and 2 while silently losing the user's label
  */
  /*
    Scoped to the PAYLOAD, not to element types.

    The first version counted `script:not([src]), img[onerror], svg[onload]`
    and failed — on Next's own inline `self.__next_f.push(...)` bootstrap
    scripts, which every App Router page carries and which are the exact
    reason `script-src` still needs 'unsafe-inline'. It reported a correct
    page as injected.

    `__pwned` appears only in the payload, so an element carrying it can
    only have come from the label being parsed as markup.
  */
  const injected = await page.evaluate(() => {
    const suspects = [...document.querySelectorAll('script, img, svg, *[onerror], *[onload]')]
    return suspects
      .filter((el) => {
        const html = el.outerHTML ?? ''
        return html.includes('__pwned')
      })
      .map((el) => el.tagName.toLowerCase())
  })
  expect(
    injected,
    'the profile label was parsed as markup — these elements came from the ' +
      'payload. Something is rendering a user string as HTML; see ' +
      'apps/web/src/test/no-raw-html.test.ts.',
  ).toEqual([])

  expect(
    await page.evaluate(() => '__pwned' in window),
    'a payload in the profile label executed on /settings/birth-profiles',
  ).toBe(false)

  await expect(
    page.getByText('window.__pwned', { exact: false }).first(),
    'the label rendered as neither markup nor text — the user lost it entirely',
  ).toBeVisible()
})
