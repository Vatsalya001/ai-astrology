import { expect, test, type Page } from '@playwright/test'

import { uniquePhone, watchOTP } from './otp-log'

/**
 * PDF export and share links, end to end.
 *
 * PHASE-03 §17 asks for both and neither had an e2e test. The unit and
 * integration suites cover the handlers; what was missing is the part
 * where a browser, a real token and the real worker meet.
 *
 * Three of these are security properties rather than features:
 *
 *   • §17: "PDF request for another user's chart is rejected"
 *   • §11: "share links resolve server-side against the viewer's
 *     permissions — they do not embed birth details"
 *   • §11.6: "Rate limit PDF generation (CPU-expensive and trivially
 *     abusable)"
 *
 * Those are the kind that pass silently when they are not actually being
 * exercised, so each asserts its setup SUCCEEDED before asserting the
 * refusal. The sibling file xss-profile-label.spec.ts records what
 * happens otherwise: it once sent an unauthenticated POST, got a 401,
 * read it as "the server refused the hostile label" and passed while
 * asserting nothing.
 *
 * The same trap is live on this page specifically. SharedChartPage
 * renders ONE message — `sharedGone` — for both a 404 and an unreachable
 * API, so "the revoked link shows the gone message" is also what a dead
 * backend looks like. Every share assertion below therefore checks the
 * API's status code directly, and treats the page as confirmation that
 * the viewer sees the right thing rather than as the proof itself.
 *
 * ── Mutation evidence ──
 *
 * Green was not taken as proof. Each guard was deliberately broken and
 * the suite re-run, to confirm the matching test actually fails:
 *
 *   RequireProfileOwnership — dropped the `return` after Owns() fails
 *     → "a PDF request for another user's chart is rejected" FAILED (got 201)
 *     → "a stranger cannot list or revoke ..." FAILED at the list step
 *   RenderLimit.Max 10 → 100000
 *     → "the PDF route is rate limited" FAILED (14 accepted)
 *   RevokeChartShare SQL — `user_id = $2` replaced with a tautology
 *     → "a stranger cannot list or revoke ..." FAILED at the REVOKE step
 *
 * The third was run with the first reverted, on purpose: while ownership
 * was broken the list assertion fired first and masked the revoke one,
 * so revoke scoping had not actually been proven by the earlier round.
 *
 * One test PASSED under the ownership mutation and should have:
 * "polling another user's PDF job returns nothing" is guarded by the
 * status key being composed from the authenticated user, not by the
 * middleware — so its surviving is what tells us it tests what it says.
 */

const API_URL = process.env.API_URL ?? 'http://localhost:4000'

/** What the viewer is told when a link does not resolve, from dictionaries.ts. */
const GONE = /this link is no longer available/i

interface Account {
  token: string
  profileID: string
}

/** Signs up, creates one birth profile, and returns a usable token. */
async function newAccountWithProfile(page: Page, tail: string): Promise<Account> {
  const phone = uniquePhone(tail)
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

  // The access token is held in memory by design (localStorage is
  // readable by any script on the page), so it is minted the way the app
  // does — the refresh cookie rides on page.request.
  const refreshed = await page.request.post(`${API_URL}/api/v1/auth/refresh`, {
    data: {},
    failOnStatusCode: false,
  })
  expect(
    refreshed.status(),
    'could not mint an access token — a setup failure, not a result',
  ).toBe(200)
  const token = ((await refreshed.json()) as { access_token: string }).access_token

  // "Jaipur" because tests/fixtures/places/cities-e2e.txt is twenty
  // cities and the CI gazetteer holds only those; a hardcoded GeoNames id
  // would rot the day it is reseeded. Both halves of that lesson are
  // written up in xss-profile-label.spec.ts, which learned them the hard
  // way in both directions.
  const places = await page.request.get(`${API_URL}/api/v1/places/search?q=Jaipur`, {
    headers: { Authorization: `Bearer ${token}` },
    failOnStatusCode: false,
  })
  expect(places.status(), 'place lookup failed, so the profile below is not valid').toBe(200)
  const placeID = ((await places.json()) as { places: Array<{ id: number }> }).places[0]?.id
  expect(placeID, 'no place matched "Jaipur" — is the gazetteer seeded?').toBeTruthy()

  const created = await page.request.post(`${API_URL}/api/v1/birth-profiles`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      label: 'Export subject',
      birth_date: '1994-08-17',
      birth_time: '14:35',
      time_accuracy: 'exact',
      place_id: placeID,
    },
    failOnStatusCode: false,
  })
  expect(created.status(), 'could not create a birth profile').toBeLessThan(300)
  const profileID = ((await created.json()) as { id: string }).id
  expect(profileID).toBeTruthy()

  return { token, profileID }
}

function auth(token: string) {
  return { headers: { Authorization: `Bearer ${token}` } }
}

// ─── PDF ─────────────────────────────────────────────────────────────

test('a PDF export runs through the worker and produces a real PDF', async ({ page }) => {
  const me = await newAccountWithProfile(page, '811')

  const started = await page.request.post(`${API_URL}/api/v1/charts/${me.profileID}/pdf`, {
    ...auth(me.token),
    data: {},
    failOnStatusCode: false,
  })
  // 202, and asserted exactly: the handler is deliberate that the work
  // has been ACCEPTED and not done, and a 200 slipping in here would be
  // a lie a client is entitled to act on.
  expect(started.status(), 'the PDF route refused a legitimate request').toBe(202)
  const jobID = ((await started.json()) as { job_id: string }).job_id
  expect(jobID, 'no job id came back, so there is nothing to poll').toBeTruthy()

  // Polled rather than slept: a fixed wait either flakes on a slow
  // machine or wastes the time on a fast one, and this render starts a
  // real browser.
  let body: { status: string; url?: string; message?: string } = { status: '' }
  for (let i = 0; i < 40; i++) {
    const status = await page.request.get(
      `${API_URL}/api/v1/charts/${me.profileID}/pdf/${jobID}`,
      { ...auth(me.token), failOnStatusCode: false },
    )
    expect(status.status(), 'the status route stopped answering mid-poll').toBe(200)
    // The body carries a signed URL to private birth data.
    expect(status.headers()['cache-control'], 'a PDF status body must never be cached').toContain(
      'no-store',
    )
    body = (await status.json()) as typeof body
    if (body.status !== 'queued' && body.status !== 'running') break
    await page.waitForTimeout(1500)
  }

  /*
    `done`, not "either terminal state".

    The looser assertion was the first version, on the theory that a
    render needs a browser and CI might not have one. CI does: the
    workflow sets CHROME_PATH from Playwright's own chromium before
    starting the worker, precisely so this works. Accepting `failed`
    would mean the one test that drives the whole pipeline could not tell
    a working export from a broken one.
  */
  expect(body.status, `render did not finish: ${body.message ?? '(no message)'}`).toBe('done')
  expect(body.url, 'a finished render with no URL is not a download').toBeTruthy()

  // And it is genuinely a PDF. A signed URL to a zero-byte object, or to
  // an error page the storage layer returned with a 200, would satisfy
  // every assertion above.
  const download = await page.request.get(body.url!, { failOnStatusCode: false })
  expect(download.status(), 'the signed download URL did not serve').toBe(200)
  const bytes = await download.body()
  expect(
    bytes.subarray(0, 5).toString('latin1'),
    'the object at the download URL is not a PDF',
  ).toBe('%PDF-')
  // A chart PDF is ~110 KB. The floor only catches a header-and-nothing
  // file, which is what a failed render that still uploaded would leave.
  expect(bytes.length, 'the PDF is too small to contain a chart').toBeGreaterThan(10_000)
})

test("a PDF request for another user's chart is rejected", async ({ page, browser }) => {
  const owner = await newAccountWithProfile(page, '812')

  // A genuinely separate context: a second account in the same one would
  // share the refresh cookie and quietly become the first.
  const other = await browser.newContext()
  const otherPage = await other.newPage()
  const stranger = await newAccountWithProfile(otherPage, '813')

  // The setup has to be real or the refusal proves nothing — a 404 for a
  // profile that never existed is indistinguishable from a 404 for one
  // the caller may not see, which is the whole design of the guard.
  const own = await otherPage.request.post(
    `${API_URL}/api/v1/charts/${stranger.profileID}/pdf`,
    { ...auth(stranger.token), data: {}, failOnStatusCode: false },
  )
  expect(own.status(), 'the stranger cannot export their OWN chart, so this proves nothing').toBe(
    202,
  )

  const theft = await otherPage.request.post(`${API_URL}/api/v1/charts/${owner.profileID}/pdf`, {
    ...auth(stranger.token),
    data: {},
    failOnStatusCode: false,
  })

  // 404, not 403 — `.claude/rules/security.md`: a 403 confirms the
  // resource exists, which is itself the disclosure.
  expect(theft.status(), "another user's chart was exportable").toBe(404)

  await other.close()
})

test("polling another user's PDF job returns nothing", async ({ page, browser }) => {
  // The ownership middleware cannot cover this one: it guards the profile
  // in the path, and a job id is not a profile. The protection is that
  // the status key is composed from the AUTHENTICATED user (pdf/status.go
  // statusKey), which is a different mechanism and so needs its own test.
  const owner = await newAccountWithProfile(page, '817')

  const started = await page.request.post(`${API_URL}/api/v1/charts/${owner.profileID}/pdf`, {
    ...auth(owner.token),
    data: {},
    failOnStatusCode: false,
  })
  expect(started.status()).toBe(202)
  const jobID = ((await started.json()) as { job_id: string }).job_id

  const other = await browser.newContext()
  const otherPage = await other.newPage()
  const stranger = await newAccountWithProfile(otherPage, '818')

  // Their OWN profile in the path, so the ownership middleware is
  // satisfied and cannot be what produces the 404 — the stranger's job id
  // is the only thing being tested. Without this the test would pass on
  // the wrong guard.
  const peek = await otherPage.request.get(
    `${API_URL}/api/v1/charts/${stranger.profileID}/pdf/${jobID}`,
    { ...auth(stranger.token), failOnStatusCode: false },
  )
  expect(peek.status(), "one user could poll another user's render job").toBe(404)

  await other.close()
})

// ─── share links ─────────────────────────────────────────────────────

test('a share link resolves, then stops the moment it is revoked', async ({ page, browser }) => {
  const me = await newAccountWithProfile(page, '814')

  const created = await page.request.post(`${API_URL}/api/v1/charts/${me.profileID}/shares`, {
    ...auth(me.token),
    data: { expires_in_days: 30 },
    failOnStatusCode: false,
  })
  expect(created.status(), 'could not create a share link').toBe(201)
  // The only copy of the plaintext token that will ever exist — a shared
  // cache holding this body would hold a working credential to a birth
  // chart, retrievable after the owner revoked it.
  expect(created.headers()['cache-control'], 'the token response must not be cached').toContain(
    'no-store',
  )
  const share = (await created.json()) as { id: string; token: string }
  expect(share.token, 'no token returned — it is shown once, at creation').toBeTruthy()

  const shared = `${API_URL}/api/v1/shared/${share.token}`

  // Unauthenticated on purpose: `credentials: 'omit'` is how the page
  // fetches, and a developer's live session must not make this look
  // correct when it is not.
  const before = await page.request.get(shared, { failOnStatusCode: false })
  expect(before.status(), 'a freshly-created link did not resolve').toBe(200)
  expect(
    before.headers()['x-robots-tag'],
    'a share link WILL end up somewhere a crawler can see it',
  ).toContain('noindex')

  // And the viewer — no account at all, which is the only caller that
  // matters for this route.
  const anon = await browser.newContext()
  const viewer = await anon.newPage()
  await viewer.goto(`/shared/${share.token}`)
  await expect(
    viewer.getByText('Export subject'),
    'the chart did not render for a viewer holding a valid link',
  ).toBeVisible({ timeout: 15_000 })

  const revoked = await page.request.delete(
    `${API_URL}/api/v1/charts/${me.profileID}/shares/${share.id}`,
    { ...auth(me.token), failOnStatusCode: false },
  )
  expect(revoked.status(), 'revoke was refused').toBe(200)

  // The API first, because it is unambiguous. The page shows one message
  // for "revoked" and for "the API is down", so asserting only there
  // would pass against a dead backend.
  const after = await page.request.get(shared, { failOnStatusCode: false })
  expect(after.status(), 'a REVOKED share link still resolved').toBe(404)

  await viewer.goto(`/shared/${share.token}`)
  await expect(viewer.getByText(GONE)).toBeVisible({ timeout: 15_000 })
  await expect(
    viewer.getByText('Export subject'),
    'the revoked chart was still on the page',
  ).toHaveCount(0)

  await anon.close()
})

test('a share link carries no birth details, and the payload does not either', async ({ page }) => {
  const me = await newAccountWithProfile(page, '815')

  const created = await page.request.post(`${API_URL}/api/v1/charts/${me.profileID}/shares`, {
    ...auth(me.token),
    data: { expires_in_days: 30 },
    failOnStatusCode: false,
  })
  expect(created.status()).toBe(201)
  const { token } = (await created.json()) as { token: string }

  // §11: share links "do not embed birth details". A token encoding the
  // date or place would put PII into every browser history, referrer
  // header and chat app the link is pasted into —
  // `.claude/rules/security.md` treats birth date + time + place as close
  // to a unique identifier, so that is the same class of leak as an email
  // address in a query string.
  const secrets = ['1994', '08-17', '1708', '14:35', '1435', 'Jaipur', 'jaipur']
  for (const secret of secrets) {
    expect(token, `the share token embeds ${secret}`).not.toContain(secret)
  }
  expect(token.length, 'a short token is guessable').toBeGreaterThan(20)

  // The response body is the other half, and the more likely leak: the
  // page comment claims "the API does not send them, and this page could
  // not display them if it wanted to". That is a claim about the payload,
  // so it is worth checking the payload.
  const body = await (await page.request.get(`${API_URL}/api/v1/shared/${token}`)).text()
  for (const secret of ['1994-08-17', '14:35', 'Jaipur']) {
    expect(body, `the shared payload leaks ${secret}`).not.toContain(secret)
  }
})

test('a share token that never existed is refused, and gives nothing away', async ({
  page,
  browser,
}) => {
  // The negative case. Without it, an endpoint that 404'd EVERY token
  // would satisfy the revoke test above.
  const bogus = 'definitely-not-a-real-share-token-000000000000'

  const res = await page.request.get(`${API_URL}/api/v1/shared/${bogus}`, {
    failOnStatusCode: false,
  })
  expect(res.status(), 'an invented token did not 404').toBe(404)

  // It must not distinguish "never existed" from "revoked" or "expired":
  // learning which would tell a prober that a link was once real and that
  // its owner turned it off. Same reasoning as the single
  // ErrRefreshInvalid in the auth package.
  const text = (await res.text()).toLowerCase()
  for (const tell of ['revoked', 'expired', 'never', 'unknown token', 'no such']) {
    expect(text, `the 404 body distinguishes this case: "${tell}"`).not.toContain(tell)
  }

  const anon = await browser.newContext()
  const viewer = await anon.newPage()
  await viewer.goto(`/shared/${bogus}`)
  await expect(viewer.getByText(GONE)).toBeVisible({ timeout: 15_000 })
  await anon.close()
})

test("a stranger cannot list or revoke another user's share links", async ({ page, browser }) => {
  const owner = await newAccountWithProfile(page, '819')

  const created = await page.request.post(`${API_URL}/api/v1/charts/${owner.profileID}/shares`, {
    ...auth(owner.token),
    data: { expires_in_days: 30 },
    failOnStatusCode: false,
  })
  expect(created.status()).toBe(201)
  const share = (await created.json()) as { id: string; token: string }

  const other = await browser.newContext()
  const otherPage = await other.newPage()
  const stranger = await newAccountWithProfile(otherPage, '820')

  const listed = await otherPage.request.get(
    `${API_URL}/api/v1/charts/${owner.profileID}/shares`,
    { ...auth(stranger.token), failOnStatusCode: false },
  )
  expect(listed.status(), "a stranger could list another user's share links").toBe(404)

  // Revoke is the one that matters most: it is addressed by share id, so
  // a handler that looked the share up without scoping it to the caller
  // would let anyone turn off anyone's link. The stranger's own profile
  // is in the path so the ownership middleware is satisfied and cannot be
  // what produces the refusal.
  const killed = await otherPage.request.delete(
    `${API_URL}/api/v1/charts/${stranger.profileID}/shares/${share.id}`,
    { ...auth(stranger.token), failOnStatusCode: false },
  )
  expect(killed.status(), "a stranger could revoke another user's share link").toBe(404)

  // And it really is still alive — otherwise the assertion above would
  // hold even if the revoke had gone through and merely reported 404.
  const still = await page.request.get(`${API_URL}/api/v1/shared/${share.token}`, {
    failOnStatusCode: false,
  })
  expect(still.status(), 'the refusal reported 404 but revoked the link anyway').toBe(200)

  await other.close()
})

// ─── the management screen ───────────────────────────────────────────

test('the settings screen lists a share link and can actually revoke it', async ({
  page,
  browser,
}) => {
  /*
    The loop that had no UI at all.

    `listShares` and `revokeShare` existed in astrology-api.ts with zero
    call sites, so a user could mint a link and had no way to see it or
    turn it off — and revocation is the owner's only remedy. This drives
    the screen the way a person does, and then checks the LINK is dead
    rather than trusting what the page says about it.
  */
  const me = await newAccountWithProfile(page, '821')

  const created = await page.request.post(`${API_URL}/api/v1/charts/${me.profileID}/shares`, {
    ...auth(me.token),
    data: { expires_in_days: 30 },
    failOnStatusCode: false,
  })
  expect(created.status()).toBe(201)
  const share = (await created.json()) as { id: string; token: string }

  await page.goto('/settings/shares')

  const row = page.locator('li', { hasText: 'Export subject' })
  await expect(row, 'the link did not appear on the management screen').toBeVisible({
    timeout: 15_000,
  })
  await expect(row.getByText('Active')).toBeVisible()

  // The screen must not show the token: the listing endpoint omits it by
  // design, and a page that displayed one would mean the plaintext had
  // been stored somewhere it should not be.
  await expect(page.locator('body')).not.toContainText(share.token)

  await row.getByRole('button', { name: /^Revoke/ }).click()
  await expect(row.getByText('Revoked')).toBeVisible({ timeout: 15_000 })

  // The assertion that matters. The badge changing is a claim by the
  // page; this is the link itself, fetched with no session at all.
  const anon = await browser.newContext()
  const check = await anon.request.get(`${API_URL}/api/v1/shared/${share.token}`, {
    failOnStatusCode: false,
  })
  expect(check.status(), 'the screen said Revoked but the link still resolves').toBe(404)
  await anon.close()
})

// ─── rate limits ─────────────────────────────────────────────────────

test('the PDF route is rate limited', async ({ page }) => {
  /*
    §11.6: "Rate limit PDF generation (CPU-expensive and trivially
    abusable)." Each request starts a browser for up to ninety seconds,
    so an unlimited route converts one account into as many concurrent
    Chrome processes as the fleet will start.

    RenderLimit is 10/hour, keyed on the user — so this costs ten real
    enqueued renders, and there is no honest way to test a limit without
    reaching it. A fresh account per test keeps that cost off the others.
  */
  const me = await newAccountWithProfile(page, '816')

  let accepted = 0
  let limited = false
  for (let i = 0; i < 14 && !limited; i++) {
    const res = await page.request.post(`${API_URL}/api/v1/charts/${me.profileID}/pdf`, {
      ...auth(me.token),
      data: {},
      failOnStatusCode: false,
    })
    if (res.status() === 202) {
      accepted++
      continue
    }
    if (res.status() === 429) {
      limited = true
      expect(
        res.headers()['retry-after'],
        'a 429 with no Retry-After leaves the client guessing',
      ).toBeTruthy()
    }
  }

  expect(limited, '14 PDF renders in a row were all accepted').toBe(true)
  // The limiter fails OPEN by design (the status store is the same Redis,
  // so refusing as well would turn a degraded feature into a broken one).
  // Asserting the count catches the case where it was open the whole time
  // and something else produced the 429 — a global throttle, say.
  expect(accepted, `expected 10 to be accepted before the limit, got ${accepted}`).toBe(10)
})
