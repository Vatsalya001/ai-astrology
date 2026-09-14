# Phase 0 — manual test plan

Everything here is executable. Each step states what you should see, so a
disagreement between the two is a finding rather than a judgement call.

Automated suites are covered by `task verify`; this document is the part a
machine does not check — what the thing actually looks like and how it behaves
when a dependency dies.

Run order matters: 1 → 2 → 3 brings the stack up, 7 and 8 take it apart again.

---

## 0. The short version

```bash
./scripts/ayana up       # everything, in order, waiting on each healthcheck
./scripts/ayana status   # every URL and port, plus live health
./scripts/ayana test     # task verify + Playwright + smoke
./scripts/ayana down
```

`ayana up` is equivalent to sections 1–3 below. Read those anyway the first
time — knowing what the script waits for is what lets you debug it when a
step fails.

---

## 1. Infrastructure

```bash
cp .env.example .env       # once
task setup                 # idempotent; toolchains + git hooks
task up
docker compose ps
```

**Expect** six containers, all `(healthy)`:

| Container | Port | Purpose |
|---|---|---|
| `ayana-postgres` | 5433 | Postgres 16 + pgvector |
| `ayana-redis` | 6381 | cache, rate-limit windows |
| `ayana-minio` | 9000 / 9001 | S3-compatible object storage |
| `ayana-mailpit` | 1025 / 8025 | SMTP sink + web UI |
| `ayana-astro` | 8100 | deterministic chart computation |
| `ayana-ai` | 8200 | LLM orchestration |

Ports 5433 and 6381 are deliberate — the defaults are taken on this machine.

**Check nothing is exposed beyond loopback:**

```bash
ss -ltnp | grep -E ':(5433|6381|8100|8200|9000|8025)'
```

Every line must start `127.0.0.1:`. A `*:` or `0.0.0.0:` on any of these means an
internal service is reachable from the network, which breaks the rule that
`astro-service` and `ai-service` are never internet-facing.

---

## 2. Backend

```bash
task dev:api            # or: ./scripts/ayana up
curl -s localhost:4000/health | jq
```

**Expect** `"status": "ok"` and six `"ok"` checks — `ai`, `astro`, `mail`,
`postgres`, `redis`, `storage`. Latencies in single-digit milliseconds.

```bash
curl -s localhost:4000/api/v1/meta | jq
```

**Expect** phase 0, and every feature flag `false`.

---

## 3. Web

```bash
npm run dev --workspace=web      # or: npm run build -w web && npm run start -w web
```

Open <http://localhost:3000>.

---

## 4. Landing page — what to look for

**Contrast.** Every text token is asserted against every surface it renders on
by `npm run test --workspace=web`, and axe scans all three pages in
`tests/e2e/a11y.spec.ts`. Both were added after computing the ratios for the
first time found `ink.faint` at 3.36:1 and `accent.soft` at 3.55:1, against
the 4.5:1 this project commits to. If you change a colour in
`packages/ui/src/index.ts`, those tests are what tell you whether you may.

**Colour.** Background is midnight navy `#0B1026`. If any surface is grey or
white, a Tailwind token failed to resolve — Tailwind drops unknown colour
classes silently, so the build will still have passed. Confirm hard:

```js
// DevTools console
getComputedStyle(document.body).backgroundColor   // → "rgb(11, 16, 38)"
```

**The two hero buttons.** "Get your free Kundli" is gold and **disabled** (Phase
1 opens sign-up) — it must look deliberately unavailable, not broken. "View
system status" is a bordered secondary button and navigates.

```js
const b = document.querySelector('button[title*="Kundli"]')
getComputedStyle(b).backgroundColor   // → "rgb(212, 168, 87)"  gold
getComputedStyle(b).color             // → "rgb(11, 16, 38)"    navy
```

Grey instead of gold means the shadcn semantic tokens are unresolved.

**Keyboard.** Press <kbd>Tab</kbd> once from page load. A "Skip to content" link
must appear and take focus. Keep tabbing: every focusable element shows a gold
ring. If a ring is missing, that is a blocker, not a polish item.

**Motion.** Enable reduced motion (GNOME: Settings → Accessibility → Reduce
Animation; or DevTools → Rendering → Emulate `prefers-reduced-motion`). Reload.
The starfield must stop twinkling and the fade-up entrances must not play.

**Determinism.** Hard-reload several times. The star positions must be
*identical* every time, and the console must show no hydration warning. The
starfield uses a seeded PRNG precisely so server and client agree.

**Mobile.** DevTools device toolbar at 390 × 844. Cards stack to one column, the
roadmap wraps to three rows, nothing overflows horizontally.

---

## 5. Status page

Open <http://localhost:3000/status>.

**Expect** a green "All systems operational" banner and six dependency rows,
each with its own dot, latency and description. The ai-service row also shows
which model backend is configured — `openai-compatible · local` in development,
a paid provider in production. That line is the visible surface of invariant 3,
which is otherwise only enforced at startup and invisible thereafter. `astro-service`, `ai-service`,
Mail and Object storage carry a `non-critical` badge; Postgres and Redis do not.

This page is `force-dynamic` — it must never be cached. Reload and watch the
latency numbers change.

---

## 6. Degraded ≠ down

The central design claim of this phase. Verify it rather than believing it.

```bash
docker compose stop mailpit
curl -s localhost:4000/health | jq '.status'
```

**Expect** `"degraded"`, and **HTTP 200** — a non-critical dependency failing
must not fail the health endpoint:

```bash
curl -s -o /dev/null -w '%{http_code}\n' localhost:4000/health   # → 200
```

Reload `/status`. **Expect** an amber "Degraded — some services unavailable"
banner, Mail showing a red dot and the word `error`. Every other row stays
green. Note the status word is present as *text*, not colour alone.

```bash
docker compose start mailpit     # restore
```

---

## 7. API down

```bash
# stop the api process
curl -s localhost:4000/health    # connection refused
```

Reload `/status`. **Expect** a branded "Cannot reach api-service" card naming
the expected URL and the command to fix it. Not a stack trace, and not a blank
page.

Restart the API before continuing.

---

## 7b. Loading and error states

Both components are now covered by component tests (ADR-009) —
`npm run test --workspace=web`, or `task test:web`. Those assert the
contract: that `error.tsx` never renders `error.message`, that the skeleton
reserves a row per dependency, that the placeholders stay out of the
accessibility tree.

What they cannot assert is that Next actually *mounts* the boundary on a
server throw — that is framework behaviour, and reaching it needs a route
whose only job is to crash. The procedure below covers that seam, by hand.
Last walked 2026-09-14.

**Loading skeleton.** Add a delay before the fetch:

```tsx
await new Promise((r) => setTimeout(r, 4000))   // TEMP
```

Rebuild, then navigate from `/` by clicking "View system status" — a
client-side navigation, which is when `loading.tsx` renders. **Expect** the
skeleton: heading present, six grey rows at the real row height, banner at
the real banner height. Nothing should shift when the data lands.

**Error boundary.** Replace the delay with a throw that carries something
that must never be shown:

```tsx
throw new Error('TEMP: postgres://ayana:hunter2@localhost:5433/ayana')
```

**Expect** the branded "We couldn't render this page" card, a "Try again"
button, and a `Reference:` digest. **The connection string must not appear
anywhere in the HTML** — that is the whole point of not rendering
`error.message`, since a server-render failure routinely carries one.

Confirm the digest ties back to the server log:

```bash
grep -A2 '⨯ Error' .run/web.log
#   ⨯ Error: TEMP: postgres://ayana:hunter2@localhost:5433/ayana
#       at b (.next/server/chunks/ssr/_1iae6y5._.js:1:119) {
#     digest: '4042727749'
#   }
```

The digest on that line is the `Reference:` shown on the page. The user can
read the code aloud; the operator finds the stack.

> **Known limitation, and what to do about it.** The error page returns
> **HTTP 200**, not 500 — the App Router commits the response status before
> a streamed Server Component throws, so the boundary cannot change it.
>
> The consequence is narrow but real: *an uptime check that asserts on
> status codes will call this page healthy.* Assert on content instead.
> `ayana smoke` already does — its status-page check greps for "All systems
> operational", so it fails while the page is erroring. Verified: with the
> throw in place, smoke reports `✗ status page reads live API` and exits 1.
>
> For backend problems the status code is trustworthy: `/health` returns
> 503 when a critical dependency is down. It is only the rendered page that
> cannot signal failure this way.

Revert the edit and rebuild before continuing.

---

## 8. Cross-service trace correlation

One request must produce the same `trace_id` in all three services.

```bash
TID="manual-$(date +%s)"
curl -s -H "X-Trace-Id: $TID" localhost:4000/health > /dev/null

grep "$TID" .run/api.log                  # Go  (ayana up writes here)
docker compose logs astro | grep "$TID"   # Python
docker compose logs ai    | grep "$TID"   # Python
```

**Expect** a hit in each. `/health` fans out through the generated clients, so
this also proves the contract pipeline round-trips against the live services.

**PII check:** the trace ID is charset-constrained on purpose. Confirm a
poisoned one is rejected rather than logged:

```bash
curl -s -H 'X-Trace-Id: victim@example.com' localhost:4000/health > /dev/null
grep -c 'victim@example.com' .run/api.log        # → 0
```

---

## 9. Invariants

```bash
# astro-service has no LLM or HTTP egress
uv run --directory services/astro pytest tests/test_no_llm_imports.py -v

# ai-service cannot write to Postgres
task test:integration        # needs Docker; asserts SQLSTATE 42501

# secrets
task secrets                 # gitleaks over the full history
```

---

## 10. Full gate

```bash
./scripts/ayana test
```

Which runs, in order:

```bash
task verify           # lint + test + build, all three languages
npx playwright test   # 11 browser tests against the running stack
./scripts/ayana smoke # 15 assertions, including the security invariants
```

**Expect** green, and `go test` running with `-race`.

---

## Closed gaps

Found by running the steps above on 2026-09-13, fixed the same day. Kept
here because the reasoning matters more than the fix.

| # | Was | Now |
|---|---|---|
| 1 | `/health` returned raw probe errors — `dial tcp 127.0.0.1:8025: connect: connection refused` — to any unauthenticated caller | Closed vocabulary: `timeout`, `unreachable`, `unavailable`. Full error goes to the log, keyed by trace ID |
| 2 | `/no-such-page` rendered Next's unbranded default | `app/not-found.tsx` — wordmark, on-brand type, two routes out |
| 3 | No error boundary; an unhandled render error fell through to Next's default | `app/error.tsx` — never prints `error.message`, offers retry, shows `error.digest` for log correlation |
| 4 | `/status` is `force-dynamic` with no loading state — a blank wait | `app/status/loading.tsx` — skeleton shaped like the real six rows, so nothing jumps when data lands |
| 5 | Every page, including the 404, reported the landing page's `<title>` | Per-page `metadata`; `/status` also carries `robots: noindex` |

Gap 1 was the one that mattered. It is now asserted in three places, because
a leak reintroduced by a well-meaning "let's surface the real error" change
would otherwise be invisible:

- `health_test.go` → `TestHealthNeverLeaksProbeErrorDetail` marshals the
  response and greps it for the host, port, path and error text
- `smoke.spec.ts` → the same assertion against the live API
- `scripts/ayana smoke` → `/health leaks no internal topology`

Reproduce the fixed behaviour:

```bash
docker compose stop mailpit
curl -s localhost:4000/health | jq '.checks.mail'
#   { "status": "error", "latency_ms": 0, "reason": "unreachable" }

grep 'health probe failed' .run/api.log | tail -1
#   ...,"reason":"unreachable","err":"Get \"http://localhost:8025/readyz\": ...","trace_id":"..."

docker compose start mailpit
```

The client learns *that* mail is unreachable. Only the log learns *where* mail
lives.
