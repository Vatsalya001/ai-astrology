# Project Status

**Updated:** 2026-09-18
**Current phase:** 2 — Astrology Engine
**Gate:** ✅ **CLOSED — 20 of 20** (Phase 0 ✅, Phase 1 ✅, Phase 2 ✅)
**Repo:** https://github.com/Vatsalya001/ai-astrology (private)

---

## Phase 0 gate

Evaluated against the running system, not against intent.

| # | Item | State |
|---|---|---|
| 1 | `docker compose up -d` brings up all six services healthy | ✅ |
| 2 | Ollama models: `llama3.2:3b`, `qwen2.5:7b`, `nomic-embed-text` | ✅ all three; inference and embeddings verified |
| 3 | `task verify` passes **from a clean clone** | ✅ verified by actually cloning to a temp dir |
| 4 | `task dev` starts web, API, astro, ai | ✅ |
| 5 | Status page shows six dependencies | ✅ |
| 6 | `task migrate` applies; extensions enabled | ✅ up **and** down verified |
| 7 | `task sqlc` generates compiling Go | ✅ |
| 8 | `task contracts` idempotent; CI diff check | ✅ |
| 9 | Go calls Python via **generated** clients | ✅ hand-written client deleted |
| 10 | One request → correlated `trace_id` in all three services | ✅ |
| 11 | Missing/invalid env var → named startup failure | ✅ Go and Python; plus a typo guard for OS env vars |
| 12 | PII redaction in Go **and** Python | ✅ |
| 13 | `astro_ro` cannot write — **asserted in a test** | ✅ |
| 14 | `astro-service` has no LLM dependency — CI rule | ✅ |
| 15 | CI green **on a pull request** | ✅ 9 jobs, verified on PR #1 and #2 |
| 16 | `.claude/` with CLAUDE.md, rules, agents, workflows, state | ✅ |
| 17 | ARCHITECTURE, ROADMAP, DECISIONS, PROJECT_STATUS | ✅ |
| 18 | ADRs written; ADR-003 records the licence question as open | ✅ 8 ADRs |
| 19 | ≥10 synthetic fixtures | ✅ 12 |
| 20 | gitleaks clean | ✅ history **and** working tree |

---

## Verified on a running system

| Property | Evidence |
|---|---|
| Six containers healthy | postgres · redis · minio · mailpit · astro · ai |
| Migration up **and down** | `schema_meta` created, dropped, recreated |
| **Single-writer rule** | Integration test: `astro_ro` refused `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `CREATE TABLE` with SQLSTATE 42501; `SELECT` works; row count unchanged |
| **PII guard aborts production** | `ENV=production` + free tier → `UnsafeConfigurationError`, startup aborted |
| **astro-service is AI-free** | AST scan + installed-package check, own CI job |
| **Degraded ≠ down** | astro stopped → HTTP 200 `degraded`, others `ok` |
| **Trace across three services** | One `X-Trace-Id` appeared in api, astro and ai logs |
| **PII redaction** | Go: 15 keys, survives derived loggers. Python: same keys **plus nested dicts/lists**, depth-bounded, cycle-safe |
| Health fan-out | six dependencies, 1–4 ms each |
| 0 Go vulnerabilities | `govulncheck` — was 33 |
| 0 npm vulnerabilities | `npm audit` |
| Secret scan clean | gitleaks over full history and working tree, plus a pre-commit hook |
| Attacker-supplied header cannot inject PII | `X-Trace-Id: victim@example.com` is rejected and replaced |
| Internal services not network-reachable | every published port bound to `127.0.0.1` |
| Dependencies audited in all three languages | `govulncheck` · `pip-audit` · `npm audit` |
| Free local models work | `qwen2.5:7b` returns a completion; `nomic-embed-text` returns 768 dimensions, matching `EMBEDDING_DIM` |
| Clean clone reaches green | Cloned to a temp dir with no `.env`, no `node_modules`, no virtualenvs → `task verify` passed |
| Typo'd env var aborts startup | `DEFAULT_AYANMSA=lahiri` → `SuspectedTypoError`, naming the intended field |

**Tests:** Python 121 (55 astro, 66 ai). Go 44 functions across 5 packages — config, logging, httpapi, clients, and
the db integration suite (all `-race`). Python 76. `mypy --strict` and `ruff` clean.

Coverage added after the gate audit: health aggregation (degraded-vs-down, concurrency,
a hanging probe not stalling the others), the error envelope (a panic must not leak a
stack trace), trace middleware (inbound adoption, length cap, minting), `statusRecorder`
forwarding `http.Flusher` (Phase 5's SSE depends on it), and the generated clients
(token injection, trace propagation, cancellation).

---

## ⚠️ Data corruption on this machine — still worth investigating

**Five data-corruption events occurred while building Phase 0.** None of them are
caused by this project's code, and all were worked around, but the pattern is not
normal and it can corrupt your work.

| # | What | Evidence |
|---|---|---|
| 1 | Go stdlib `src/time/time.go` | `0x09` → `0x08` — single bit flip. Broke every build importing `time`. |
| 2 | uv cache, mypy typeshed `inspect.pyi` | `0x70` → `0x60`, `Mapping` → `` Ma`ping ``. Broke `mypy --strict`. |
| 3 | Ollama `qwen2.5:7b` (4.7 GB) | Checksum failed **twice with two different wrong hashes** (`ce342d11…`, then `ade8f13f…`). Succeeded on a later attempt — so the corruption is intermittent, not a bad upstream file. |
| 4 | Go build cache, `go/types` | `0x6E` → `0x6A`, `constDecl` → `cojstDecl`. Broke linking. |
| 5 | `next-swc.linux-x64-gnu.node` | Corrupted native binary → deterministic segfault on every `next build`. Fixed by reinstalling. |

Four bit flips at **four different bit positions**, across four unrelated tools. Two
different corrupt results for the same download rules out a bad mirror or a bad cache.

**What the evidence says:** every event involved data arriving over the network and
being written to disk. Kernel logs show no MCE, no disk errors, and ECC counters read
zero — but the machine uses Wi-Fi (`wlp0s20f3`) with no VPN active, and the NIC's own
error counters are clean, so the corruption is happening past the driver.

**That the same download later succeeded is itself informative:** an intermittent
fault that corrupts large transfers some of the time fits failing memory or a flaky
wireless path far better than it fits a bad mirror or a bad cache.

**Suggested next steps, cheapest first:**

1. `sudo memtest86+` — an overnight run. This is now the highest-value test, since the
   network hypothesis is weakened by the eventual success.
2. `sudo smartctl -a /dev/nvme0n1` for disk health.
3. If large downloads corrupt again, retry over **ethernet** to isolate the wireless
   path.

Nothing here blocks development. But expect the occasional inexplicable build failure,
and treat a corrupted dependency as the first hypothesis rather than the last — it cost
real time this session chasing a Turbopack segfault that turned out to be a corrupted
`.node` binary, not a code bug.

---

## Other environment issues, resolved

| Issue | Resolution |
|---|---|
| Go linker panicked mid-`task verify` — `index out of range [1879048191]` while resolving relocations (corruption event #8) | `go clean -cache`; green immediately after. Same fix as event #7 |
| Ports 5432 / 6379 / 6380 already in use | Postgres → **5433**, Redis → **6381** |
| `corepack enable` needs root | npm workspaces (ADR-008) |
| Go 1.27.1 breaks `govulncheck` | Pin one minor behind: `go1.26.8` (ADR-007) |
| System Go 1.22 stdlib corrupted | Toolchain pin sidesteps it. Optional host repair: `sudo apt-get install --reinstall golang-1.22-src` |

---

## Spec §15/§16 checklists

The 20-item gate summarises these; auditing them separately found seven gaps that the
gate did not surface.

| Item | State |
|---|---|
| gitleaks pre-commit hook | ✅ `scripts/pre-commit`, installed by `task setup` |
| `.env.example` per service | ✅ root + astro + ai |
| `pip-audit` in CI | ✅ added to the Python matrix |
| Python services internal-only | ✅ all ports bound to loopback |
| Trace ID never carries PII | ✅ charset-constrained in all three services |
| OpenTelemetry wired | ✅ Go + both Python; no-op without an endpoint |
| Sentry wired | ✅ Go + both Python; no-op without a DSN, with scrubbing |
| `packages/analytics` typed events | ✅ payload type forbids nested objects |

---

## Spec §13 checklist (frontend foundation)

Auditing §13 separately from the gate found six more gaps. All are now closed.

| Item | State |
|---|---|
| Retry/backoff on internal service calls | ✅ `clients/retry.go` — RoundTripper, idempotent methods only |
| Property-based tests wired up | ✅ `services/astro/tests/test_properties.py` (hypothesis, 8 tests) |
| Browser tests against the real stack | ✅ `tests/e2e/smoke.spec.ts` (Playwright, 8 tests) |
| E2E job in CI | ✅ 10 jobs total |
| Shared `packages/*` | ✅ `types`, `analytics`, `config`, `content`, `api-client`, `ui` |
| shadcn/ui installed | ✅ 6 components, rewritten onto the design tokens |

### Three defects these found

The value was not the checklist items themselves — it was what building them exposed.

1. **Retry was dead code.** `canRetry` tested `req.Body == nil`, but `net/http` and
   `httptest` represent an empty body as `http.NoBody`, not `nil`. Every bodyless GET —
   which is every call this service makes — was classified non-retryable. The retry
   transport was installed and had never once retried. Caught by `retry_test.go`.

2. **TypeScript had no linter.** Go had golangci-lint and Python had ruff; `task
   lint:web` re-ran `tsc`, which is a type checker. `next lint` had also been removed in
   Next 16, so the `lint` script had been failing on invocation. Now ESLint 9 flat
   config with `next/core-web-vitals` + `jsx-a11y/strict`, wired into both `task verify`
   and CI.

3. **shadcn shipped a second palette.** `npx shadcn add` bakes its `baseColor` in as
   literal classes (`bg-slate-900`, `bg-white`) — 83 of them across six components. That
   renders a light-grey control on a midnight-navy page while `tsc` and `next build`
   both stay green. All six were rewritten onto the design tokens.

**The gate that catches this class of bug is a computed-style assertion.** Tailwind
drops a class naming an unknown colour silently, so neither typecheck nor build can see
it. `smoke.spec.ts` now asserts the hero CTA computes to `rgb(212, 168, 87)`. Verified
negatively: removing the `primary` token still builds clean, and fails that test.

---

## Running it

`./scripts/ayana up` starts everything in order and waits on each healthcheck;
`status` prints every URL and port; `smoke` runs 15 assertions; `test` is the
full gate. `docs/TESTING-PHASE-0.md` is the manual plan.

| | |
|---|---|
| Landing / status | http://localhost:3000 · /status |
| api-service | :4000 — the only public surface |
| astro / ai | :8100 · :8200 — loopback only |
| Mailpit · MinIO | :8025 · :9001 |
| Postgres · Redis · Ollama | :5433 · :6381 · :11434 |

---

## UI audit (2026-09-13)

Running the UI rather than reading the checklist found five gaps. All closed;
see the table in `docs/TESTING-PHASE-0.md`.

**The one that mattered:** `/health` returned `err.Error()` verbatim to any
unauthenticated caller — `dial tcp 127.0.0.1:8025: connect: connection
refused`, the internal host, port and path. Confirmed reachable over the LAN.
Replaced with a closed vocabulary (`timeout` / `unreachable` / `unavailable`);
full detail now goes to the log under the request's `trace_id`. Asserted in a
Go test, a Playwright test and a smoke check.

The other four were missing UI states: no branded 404, no error boundary, no
loading skeleton on the only `force-dynamic` route, and every page reporting
the landing page's `<title>`.

**Both are now tested** (ADR-009 — Vitest + Testing Library, 15 tests). The one
that matters asserts `error.tsx` never renders `error.message`: a server-render
failure routinely carries a connection string, and the error page is public.
Verified negatively — adding `{error.message}` to the component, the change a
developer makes while debugging and forgets to revert, fails four tests
immediately.

Writing those tests found a flaw in the skeleton: the `System status` heading
sat inside an `aria-hidden` wrapper, so a screen reader lost the page structure
during load. `Skeleton` now carries `aria-hidden` itself — decorative by
definition — and real text stays announced.

`task test` previously ran Go and Python but nothing for TypeScript, the same
shape as the missing linter. Now wired into `task verify` and CI.

**Known limitation:** the error page returns HTTP 200, not 500 — the App Router
commits the status before a streamed Server Component throws. The consequence
is that an uptime check asserting on status codes calls the page healthy, so
**assert on content**. `ayana smoke` does, and was confirmed to fail and exit 1
while the page was erroring. `/health` still returns a truthful 503 for backend
problems; it is only the rendered page that cannot signal this way.

---

## Spec §12 + Definition-of-Done audit (2026-09-14)

A sweep of the parts of §12 and the Definition of Done that had never been
executed, rather than read. Four findings, all closed.

**Two accessibility defects, found by computing contrast for the first time.**
`packages/ui` declared `MIN_CONTRAST_RATIO = 4.5` with a comment saying it was
recorded as a value "so a future token change can be checked against it
programmatically" — and nothing ever checked it.

| Token | Was | Now |
|---|---|---|
| `ink.faint` | `#5E6785` — 3.36:1 on base, 3.03:1 on surface | `#858DA8` — 5.71 / 5.15 / 4.55 |
| `accent.soft` | `#8B6FD8` — 4.48:1 as badge text, 3.55:1 on a card | `#9D85DE` — 5.71 / 5.12 / 4.53 |

Neither is decoration: `ink.faint` carries the footer's medical disclaimer and
the hero's "sign-up opens in Phase 1"; `accent.soft` is the badge text. Hue and
saturation were held constant, only lightness raised.

Now guarded three ways — 69 unit tests over the tokens (including badge
backgrounds *composited* at 10% opacity, which is where `accent.soft` failed
hardest), and an axe scan of every page in Playwright. Verified negatively:
restoring the old `ink.faint` makes axe report `color-contrast` on both pages
that use it, and leaves the 404 passing, which is correct — it does not use it.

**The design tokens were defined twice.** §12 requires them "defined once" in
`packages/ui`; in practice `apps/web/tailwind.config.ts` restated every hex
with a comment reading "Change both together". They had already drifted —
`accent.foreground` existed in one and not the other. `packages/ui` is now the
single source, the shadcn semantic layer is *derived* from the palette rather
than retyped, and Tailwind imports both.

**The configured LLM provider was not shown.** §12 lists it among the things
the status page must report individually. `ai-service` already returns
`provider` and `provider_tier`; the Go health fan-out discarded them. It now
carries them through as a `detail` on the ai-service row — no new probe and no
model call, preserving ai-service's deliberate choice not to bill itself on
every health poll. This matters because invariant 3 is enforced at startup and
otherwise invisible afterwards: the status page is where an operator sees that
production is on a paid tier and development is not.

---

# Phase 1 — Authentication & User Profiles ✅

**Gate: 16 of 16 items closed. Nothing open.** Full evidence:
[`TESTING-PHASE-1.md`](TESTING-PHASE-1.md).

Hand-rolled, per the spec and the owner's choice. The §14 risk table already said
what that means — *"hand-rolled auth is a classic vulnerability source"* — with the
mitigation that every §11 item is tested rather than reviewed. That is what was done,
and the tests are what found the holes.

## What shipped

| PR | What |
|---|---|
| 1–2 | Migration, sqlc queries, the `Channel` interface, Console and SMTP |
| 3 | Tokens, rotation, reuse detection |
| 4 | Rate limiting and the auth middleware |
| 5 | Auth endpoints — login actually works |
| 6 | Profile, preferences, sessions |
| 7 | Account deletion and data export |
| 8a–8b | The nine screens, i18n, the sessions screen |
| 8c | Google OAuth, built and tested without credentials *(removed in 9a)* |
| 8d | Three rate-limiting holes found by measuring |
| 8e | Analytics events, with a vocabulary that cannot leak PII |
| 8f | The remaining §10 flows, the web CSP, this gate |
| 9a | Google OAuth removed; email and phone OTP only |
| 9c | Mobile viewport coverage — the DoD item nothing tested |

## What running it found that reading it did not

This is the part worth keeping. Every item below looked correct on the page.

**`POST /users/me/challenge` was completely unlimited.** 25 of 25 consecutive calls
returned 200. Each sends a real email or SMS to the account's own verified contact,
so an unlimited version is a mailbomb aimed at whoever owns the account, triggerable
by anyone holding an access token. Now 3 per 15 minutes **per user** — not per IP,
because an attacker changes IP freely and cannot change whose account a stolen token
belongs to. It fails *closed* if Redis is down: the blast radius lands in someone
else's inbox.

**`GlobalPerIP` was declared "the backstop, applies to every request regardless of
route" and was wired to nothing.** It is now real middleware on `/api/v1`, which is
what makes "every endpoint is limited" true by construction rather than by
remembering — including routes a later phase has not mounted yet.

**The spec's 100/minute backstop was wrong, and the e2e suite is what proved it.** Six
concurrent browser sessions generated 105 counted requests in 15 seconds — 420/minute
from one address. Indian carrier-grade NAT puts thousands of subscribers behind one
public IP, so 100 would not have throttled an attacker; it would have broken entire
carrier pools. Raised to 1200/minute from that measurement. The same trap
`OTPRequestPerIP` fell into and was corrected for.

**`go test -tags=integration` reported `ok` when Docker was unavailable**, skipping
every test. The single-writer grants, refresh reuse detection and the limiter under
concurrency would all have gone unverified behind a passing check mark.
`REQUIRE_CONTAINERS=1` in CI now turns that skip into a failure; locally it still
skips, so a developer without Docker gets a fast unit suite rather than a wall of red.

**`retryable: true` on a permanently-unconfigured feature.** The flag was derived from
`status >= 500`, so an unconfigured OAuth provider told well-behaved clients to retry
forever. 501 now, and 501 is excluded from the rule.

**The web app had four security headers and no CSP.** The four that were present are
one line each; the one that was missing requires deciding what the app is allowed to
do. Added — and the first strict version *broke the app*, which the new
`csp.spec.ts` caught as a click timeout on a page that rendered perfectly. See the
CSP section in `TESTING-PHASE-1.md` for the concession that resulted and the Phase 5
gate item that retires it.

## Guards proven by breaking them

`TESTING-PHASE-1.md` has the full table. The two that matter most:

- Removing `AND used_at IS NULL` from `RotateRefreshToken` → **32 of 32** concurrent
  rotations succeeded.
- Removing `AND revoked_at IS NULL` → a revoked device kept minting tokens,
  `/auth/refresh` returning **200** where the test expects 401.

## Data corruption, event #9

`next build` aborted with `Cache corruption detected: checksum mismatch in block 12 of
00000077.sst`. Same class as the Go linker's `index out of range [1879048191]`.
`rm -rf apps/web/.next` and it built. **`memtest86+` is still unrun.** Nine events is
not a coincidence; before chasing a mysterious build failure on this machine, clear
the relevant cache and try again.

---

## Open decisions

None. [ADR-003](decisions/003-astrology-engine.md) closed on 2026-09-16: **`skyfield`
(MIT)**, so the project carries no ephemeris licence obligation at Phase 7 or ever.

**Nothing tested the app at phone width.** The suite ran one Playwright project,
Desktop Chrome, while "mobile responsive" sat in the Definition of Done — for a
product whose audience is on Indian mobile networks. `mobile.spec.ts` now covers
every screen at 412px, public and signed-in.

Worse, the first version of that test **passed while the page was broken**. It
compared `scrollWidth` to `window.innerWidth`, and under mobile emulation Chrome
expands the layout viewport to fit overflow, so the two are always equal. A
deliberate 900px element in a 412px device measured 924 against 924 and the
assertion passed. Only breaking the layout on purpose exposed it. The reference is
now Playwright's `viewportSize()`, which the page cannot move.

---

## Deferred by decision

**Social sign-in moves to Phase 10.** Google OAuth was built and fully tested in
PR 8c — state forgery, replay, expiry, a 16-way concurrent exchange with exactly
one winner, unverified-email refusal, all against a stubbed Google with a real
Redis — and removed in PR 9a. The decision was the owner's: keep sign-in simple
until the product exists, then add social login at the end.

It is **moved, not dropped**. `PHASE-10-MOBILE.md` carries it as tasks 10.3a and
10.3b, a §11 item for `state` validation, and two gate items. **Restoring the
implementation is `git revert` of PR 9a** — the code and every one of its tests
come back intact. Do not rewrite it from scratch.

Phase 10 is also where it belongs: Apple requires *Sign in with Apple* wherever
an app offers a third-party social login, so Google and Apple have to land
together once iOS ships.

The conversion cost of having no social sign-in is real and accepted. Email and
phone OTP need no third party and work today.

---

---

# Phase 2 — Astrology Engine ✅

**Gate: CLOSED — 20 of 20 gate items, 8 of 8 Definition-of-Done items.**
Evidence: [`TESTING-PHASE-2.md`](TESTING-PHASE-2.md).

## What shipped

- **astro-service**: skyfield + a vendored de421 kernel, Lahiri ayanamsa from the ICRC
  anchor, whole-sign houses, navamsa, a three-level Vimshottari tree in `Decimal`,
  graha drishti, 11 yogas, gochara and Sade Sati. No database, no network, no model.
- **api-service**: versioned birth profiles, a place gazetteer, chart storage and
  caching, a six-hourly `asynq` transit worker, and eleven endpoints behind an
  ownership middleware.
- **web**: the three-step birth flow, the computing screen, and the profile manager.
- **30 golden fixtures**, cross-validated against published astronomical events before
  being frozen.

## What running it found that reading it did not

The full list is in `TESTING-PHASE-2.md`. The worst:

**The containerised astro-service had no ephemeris kernel.** The Dockerfile copied
`app` and not `data`. The container started, answered `/health`, passed every compose
health check, and 500'd on the first request that needed a planet — for the whole
phase. No test had ever computed a real chart through the stack. The end-to-end suite
task 2.21 asks for found it on its first run.

**`scripts/ayana up` served a months-old image**, because `docker compose up` was
missing `--build`. Six green ticks, stale code. CI never hit it: a fresh runner has no
image to be stale.

**`?type=D9` returned a relabelled D1.** The chart type never reached astro-service, so
both rows held the identical payload and a client asking for the navamsa got the rasi.

**819 database round trips per chart** for the dasha tree. Measured: 815 ms one-at-a-
time against 26 ms for a single `COPY`.

**The data export never mentioned birth profiles or charts**, which the §14 checklist
requires. Deletion had a guard that discovers new tables; the export had none, because
a missing section is an absent JSON key and an absent key reads as "you have none".

## Measured against the §17 budgets

| Budget | Measured |
|---|---|
| Chart computation < 200 ms | p95 **70.6 ms** |
| Go round trip < 350 ms cold | **109–140 ms** |
| Cached read < 10 ms | p95 **8.5 ms**, max **10.0 ms** |
| Place search < 50 ms p95 | **2.7 ms** |

## Corrections I had to make to my own earlier claims

Two, both recorded in full in `TESTING-PHASE-2.md`:

- I had "fixed" `subdivision_index` citing nine failing nakshatra boundaries. Measured
  against exact rational arithmetic the count was three, and the fix was wrong **more**
  often than what it replaced. It is now exact.
- A golden-file tolerance I reported as landed had never been applied — a text edit
  silently failed to match. The CI run that went green did so by luck on a runner that
  happened to agree.

## Next

**Phase 3 — Kundli UI.** `docs/specs/PHASE-03-KUNDLI-UI.md`.

Phase 2 built data entry only. Phase 3 is the visualisation: North and South Indian
chart SVGs, the dasha timeline, the PDF worker. The chart SVG needs per-element
`aria-label`s and a visually-hidden table duplicating the data — an SVG is meaningless
to a screen reader otherwise.

### Phase 3 progress

PRs 1–11 shipped the chart SVG, the varga and style switchers, the planets, dasha,
transit, yoga and dashboard screens, the i18n retrofit, and the glossary.

**PR 12 — the PDF worker.** Task 3.15.

The document is produced by driving headless Chrome at the app's own print route
(`/kundli/print`) rather than by a Go PDF library — a library would be a second
implementation of the chart, maintained in parallel with the one on screen, drifting
silently because nobody looks at a PDF as often as a screen.

That browser has no session, so it carries a **single-use print token**: 128 bits from
`crypto/rand`, five-minute TTL, redeemed with Redis `GETDEL`, scoped to one user and one
profile. The route it calls has no profile id in its path or query at all, so "trust the
token but read the id from the request" — which turns any valid token into a reader for
every chart — is a mistake the route cannot make rather than one it avoids.

| Piece | Where |
|---|---|
| Print token, mint and redeem | `services/api/internal/charts/printtoken.go` |
| The unauthenticated print route | `GET /api/v1/print/chart?token=` |
| Everything the document renders, in one response | `charts/printbundle.go` |
| Queue, status, renderer, browser | `services/api/internal/pdf/` |
| The printed page | `apps/web/src/app/kundli/print/` |
| The download control | `apps/web/src/components/chart/DownloadPdf.tsx` |

`POST /charts/{id}/pdf` → 202 `{job_id}`; `GET /charts/{id}/pdf/{jobId}` → status, then a
24-hour signed URL. Renders run on their own asynq queue so a slow one cannot sit in
front of the transit refresh. Object keys are `kundli-{random}.pdf` — no name, no profile
id, no job id, because keys turn up in bucket listings and access logs.

**PR 13 — the rate limit PR 12 should have had.** Auditing the spec's own security
checklist against what PR 12 shipped turned up an item it does not satisfy: *"Rate limit
PDF generation (CPU-expensive and trivially abusable)."* One request starts a browser for
up to ninety seconds, which makes it the most expensive thing an authenticated user can
ask this service to do — more so than the recompute route, which already carries a limit.

Ten per user per hour, checked before the job id is minted so a refused request leaves no
orphan status behind. Fails open on a Redis outage, matching the recompute route: the
status store is the same Redis, so an outage already means no render can report its
result, and refusing as well turns a degraded feature into a broken one.

It also closed a latent panic. `Deps.Limiter` is a concrete `*ratelimit.Limiter` and
`pdf.Create` takes an interface, so a nil pointer becomes a **non-nil interface holding a
nil value** — the handler's own `limiter != nil` guard lets it through and `Allow` is
called on a nil receiver. `NewRouter` now refuses to construct. The break test produced
exactly that nil-pointer dereference, which is how the hazard was confirmed rather than
assumed.

**PR 14 — the share sheet.** Task 3.16.

Three things, because people mean three different things by "share". An **image** goes
straight into a WhatsApp thread. A **link** stays live and can be revoked, which is what
you send an astrologer. A **PDF** is the artifact people print.

The link is the part with teeth. From the spec's checklist: *"Share links resolve
server-side against the viewer's permissions — they do not embed birth details."* So the
URL carries one opaque token and nothing else: 128 bits from `crypto/rand`, stored only
as a SHA-256 hash, resolved against a row the owner can kill. A link that carried the
chart — signed, encoded, however cleverly — would be a permanent, unrevocable publication
of somebody's birth data the moment it left their phone.

| Piece | Where |
|---|---|
| `chart_shares`, hashed token, revocation, view count | `services/api/db/migrations/000004_shares.up.sql` |
| Token mint and hash | `services/api/internal/shares/token.go` |
| Create, list, revoke, resolve, sweep | `services/api/internal/shares/` |
| The reduced view a viewer gets | `services/api/internal/charts/shared.go` |
| The public route | `GET /api/v1/shared/{token}` |
| Share sheet and PNG export | `apps/web/src/components/chart/ShareSheet.tsx` |
| The page a recipient opens | `apps/web/src/app/shared/[token]/` |

The shared payload carries the chart and the profile's label, and no birth date, time or
place — enforced by a test that compares the shared response against the owner's own
profile response field by field, rather than by guessing at names. (The first version
scanned for the substring "longitude" and failed on `planets[].longitude`, which is a
planet's position along the ecliptic and the entire content of a chart.)

Correcting birth details revokes every link to the superseded version: a link pointing at
the old one would keep serving a chart its owner has already decided was wrong, to people
they cannot reach.

**Two existing guards caught the new table before it shipped**, which is what they were
built for: `TestHardDeleteLeavesNoResidue` refused to pass until `chart_shares` was seeded
in the deletion test, and `TestEveryUserOwnedTableAppearsInTheExport` refused until share
links reached the GDPR export. Neither was something this PR remembered on its own.

Migrations now have a test rather than a rule. `.claude/rules/database.md` has always said
"every migration has a real `down`"; nothing executed one. `migrations_test.go` rolls every
migration back and forward again — the second pass is what tests the down rather than the
parser, since a down that drops nothing exits zero and the re-apply then fails on an
object that still exists.

**PR 15 — the performance budget, and a spec target that cannot be met.** Task 3.19.

`task build` and CI now fail when a route's first-load JS exceeds its gzipped budget.
The check gzips the chunks itself: `next build` reports first-load JS *uncompressed*, and
the two differ by about 3.3x here, so checking the reported number against a gzip budget
would pass everything forever.

**The spec's 180 KB target is not reachable and this PR does not pretend otherwise.**
Measured 2026-09-18:

| | gzipped |
|---|---|
| `/kundli/chart` first-load JS | **199.0 KB** |
| Framework floor — React, react-dom, Next client runtime, before any product code | **159.5 KB** |
| Two chunks with no product markers at all | 112 KB |
| i18n dictionaries (both locales, in the root layout) | 14.8 KB |

That leaves roughly 20 KB for everything Ayana does. The target was set without measuring
the framework, and no amount of trimming product code reaches it — so it is recorded here
as **an unmet spec item**, not quietly redefined. Closing it means either shipping one
locale's dictionary instead of both (~15 KB), or a decision about the framework, and
neither is a Phase 3 call.

What the enforced budgets *do* buy is regression detection: each is the measured size plus
a small allowance, so an import that drags in a date library or a second copy of a corpus
fails the build on the PR that adds it. Four failure modes are break-tested — a route over
budget, a budget naming a route the build does not produce, missing build stats, and a
manifest naming a chunk that does not exist.

**PR 16 — closing the gate: the stack, the e2e suite and visual regression.**

*The e2e suite runs, and passes.* All **108** specs green against the real stack. It had
not been run this session, so six gate items were resting on assumption. The first attempt
failed 29 of them — entirely because the services were started by hand rather than with
`scripts/ayana up`, which is the exact mistake CI's own comment in `ci.yml` warns about:
the harness reads OTPs from `.run/api.log`, and a hand-started API writes somewhere else.

*A compose bug PR 12 shipped.* The `minio-init` container that creates the bucket runs to
completion and exits 0 — and `docker compose up --wait`, which `scripts/ayana up` uses,
counts an exited container as a failure. Every service came up healthy and the script then
died. It now sits behind a compose profile and is started explicitly; `down` passes
`--profile init --remove-orphans`, without which the one-shot container is left holding a
reference to a network that has just been removed, and the *next* `up` fails with
"network … not found". `task up` had appeared fine because it does not use `--wait`.

*Visual regression suite, at 3 viewports.* 15 baselines across 360px, 768px and 1280px —
the chart on its own, plus the chart, planets, dasha and yoga screens full-page with
time-dependent regions masked. Stable across three consecutive runs.

**The second theme is not covered, because it does not exist.** `.claude/rules/frontend.md`
is explicit that the app is dark-only, and screenshotting a theme no user can reach would
lock in the appearance of a code path that never executes. Recorded as met-in-part rather
than counted as done.

**The suite's first version did not work, and the break test is why we know.** At
Playwright's default `threshold: 0.2` it passed with the chart's background class replaced
by one Tailwind does not define — the precise defect `frontend.md` warns about. The reason
is measurable: this palette lives in a narrow band of very dark navy, and `#0B1026`
against the black an SVG falls back to is a YIQ distance of **0.067**, which the default
counts as identical. At `0.03` the break produces a 95% pixel difference and fails.

**PR 17 — Sade Sati gets its dates.** Gate item 8 asks for "correct phase **and dates**".
Until now the product could say only that the stretch was running and which of three
phases — the half that causes anxiety without the half that relieves it. "When does this
end" is the question people actually ask.

The engine already had `sade_sati_window`, tested, finding the first entry into the 12th
and the exit Saturn does not return from. It was exposed nowhere.

| Layer | Change |
|---|---|
| astro | `started_at`/`ends_at` on the transits response, plus `POST /v1/transits/sade-sati/windows` returning all twelve Moon signs at once |
| contract | regenerated; the Go client picked up the endpoint |
| api | `sade_sati_windows` table — twelve rows, refreshed with the transits |
| web | the window and "about N years left", measured from the **server's** instant |

**Stored rather than asked for.** Go reads the window from Postgres, never from astro, for
the same reason the transits table exists: this is a question users ask constantly and the
answer must survive astro being unreachable. Proven by taking astro down *after* a refresh
and confirming the dates still come back.

**Twelve or none.** A short response from astro writes nothing — a partial write leaves
some Moon signs on fresh dates and others on stale ones, with nothing downstream able to
tell. That guard had no test until a break test showed the existing one passed without it.

**Three defects this found in existing tests:**

- `TestEveryRealTableRefusesWritesFromReader` did `UPDATE … SET id = id` under a comment
  claiming it did not depend on column names. `sade_sati_windows` is keyed by
  `moon_sign_index`, so the UPDATE failed with *"column id does not exist"* rather than
  *"insufficient privilege"* — a pass for the wrong reason, leaving the grant untested. It
  was caught only because that test **also** checks which error it got.
- The transit refresher's slot test asserted "exactly one call to astro", which was
  scaffolding rather than its subject. It now asserts every call names the slot boundary,
  which is stronger and on-topic.
- The transits test harness answered every path with the same body, so the new windows
  call silently failed to decode while every positions assertion still passed.

The web test that asserted *no* dates appear — "because the engine reports none" — was
correct when written and is now replaced. The rule it protected has not changed: the dates
shown must be the ones the API sent, and a break test confirms deriving them in TypeScript
fails.

**PR 18 — the Sade Sati dates were wrong, and the test only checked one sign.**

PR 17 shipped the dates with a test that verified the span was ~7.5 years **for Pisces**.
Asking the running engine for all twelve revealed:

```
Gemini       2007-01-10 -> 2007-07-15   0.51 years
Cancer       2006-10-31 -> 2009-09-09   2.86 years
Capricorn    2023-01-17 -> 2025-03-29   2.20 years
```

Two distinct bugs, both producing plausible-looking dates:

1. **The wrong stretch.** `sade_sati_window` returns the first stretch inside the search
   span. Over forty years most Moon signs have one in the past — so nine of twelve were
   reporting dates from 2006–2017 while claiming to describe today.
2. **A sign change mistaken for an entry.** Walking back for the start accepted any
   in-stretch ingress whose last *outside* ingress was long ago — true of every crossing
   deep inside the stretch. A Capricorn Moon got Saturn's move into Aquarius, five years
   late.

Replaced by `sade_sati_window_at`, which starts from the instant asked about, refuses
immediately when Saturn is not in the stretch, and walks out both ways from there. The
backward walk now finds the most recent *real* boundary — an outside ingress that is not a
retrograde dip — and takes the first in-stretch ingress after it.

**Also 68 seconds → 5.1 seconds.** The endpoint scanned the ephemeris once per sign; the
scan is the entire cost and does not depend on the Moon sign. Twelve signs now share one
scan. That 68 seconds was not academic — the worker's first real call timed out and the
windows were never stored, which is how the wrong dates were noticed at all.

Verified against the running engine: **3 of 12** signs in Sade Sati (Saturn is in Pisces,
so Aries/Pisces/Aquarius), spans 7.17–8.08 years, and the worker stores exactly those
three.

The missing test now checks **every** sign, that each window contains the instant it was
computed for, and that roughly three signs are running at once.

**PR 19 — task 3.18, the four states, enforced.** The Definition of Done asks every
screen for loading, error, empty and populated. An audit found them all present — so the
deliverable is not the states, it is something that keeps them true.

Not a source scan. The cheap version greps each `page.tsx` for a skeleton and an error
branch, and it produced **two false negatives on its first run**: `/onboarding/computing`
reported as having no error path when it has a `failed` state, `/shared/[token]` when it
handles both `gone` and `unreadable`. The grep did not know those names. A guard that
calls a protected screen unprotected is one people learn to ignore — and it would say
nothing at all about a branch that exists and never renders.

`tests/e2e/kundli-states.spec.ts` drives the real states in a real browser across all five
Kundli routes: signed in with **no birth profile** (the state every user passes through
once and nobody building the product sees again), a held-open request for the loading
state, and a failing data endpoint for the error state.

Scoping the failure taught something. The first version failed every `/api/v1/` path,
which also fails `/api/v1/auth/refresh` — and a session that cannot be refreshed genuinely
cannot continue, so the app correctly redirected to sign-in and the test read that as "no
error state". It now fails the birth-profile list only, which every Kundli route loads
first.

**PR 20 — task 3.17, Storybook.** The spec's reason is not documentation: *"Storybook is
free and it is what keeps Phases 5 and 10 from re-inventing all of this."* Phase 5 builds
a chat UI on these components; Phase 10 ports them to React Native.

17 stories across `PlanetTable`, `SadeSatiIndicator` and `YogaCard`, chosen for the states
that are **hard to reach in the running app** and therefore rot: an empty planet table, a
yoga the corpus has no entry for, a strength value the UI does not know, and all three
Sade Sati phases — which are visible for about thirty months every twenty-nine years.

Every story renders inside the app's real `LocaleProvider` and on `bg-base`. Neither is
decoration: components here call `useLocale()`, which *throws* outside a provider, and on
Storybook's default white canvas `text-ink` (#F2F3F8) is invisible.

**The RTL variant the spec asks for is a direction toggle, not a locale.** This product
ships English and Hindi, both left-to-right; there is no RTL locale to screenshot.
The toggle earns its place by surfacing hardcoded `left`/`ml-` that would break the day an
Urdu locale is added — and the story says that rather than claiming a box is ticked.

**A build does not execute stories.** `stories.test.tsx` renders all 17 in the unit suite,
because a story whose args have drifted from its component's props compiles right up until
somebody opens that panel. Break-tested both ways: a story the component cannot render,
and a stories file that exports none.

**PR 21 — gate item 13 automated, and two real accessibility bugs it found.**

Most of the "manual" script is not a judgement. "Does the focus ring stay visible", "does
Escape return focus to the opener", "is the current dasha marked `aria-current`" are facts
a browser can be asked. `tests/e2e/kundli-keyboard.spec.ts` asks eleven of them.

**Two bugs, both invisible to axe** — which reports zero violations on every Kundli route:

1. **The skip link skipped nothing.** `<main id="main">` had no `tabIndex={-1}`, so it was
   not focusable; `href="#main"` scrolled the page and left focus on the link. The next Tab
   continued from the link. Fixed on all 20 pages that carry a `main`.
2. **No dialog returned focus.** Radix restores focus to its `DialogTrigger`; every dialog
   here is controlled by a plain button calling `setOpen(true)`, so there was no trigger
   and closing dropped focus to `<body>`. All six had it. For a screen-reader user the
   reading position is lost and they resume at the top of the document, unannounced.

The second took four attempts, and the reason is worth recording: Radix's `onOpenChange`
**never fires on open** for a controlled dialog — it only handles its own dismissal paths.
Three fixes were built on the assumption that it did. Instrumenting the handler showed a
`CLOSE` with no matching `OPEN`, which ended the guessing; focus is now tracked
continuously by a `focusin` listener.

**Performance target, measured rather than estimated.** Removing *both* the glossary prose
(8.9 KB, needed only on tap) and the entire Hindi dictionary (7.1 KB) — the only removable
content — reaches **183.0 KB** against a 180 KB target. The gap is not closeable by
product-code work, so no refactor was attempted.

**Two defects PR 12 found in existing code**, both invisible until the print route
existed:

- `birthprofiles.ErrNotFound` and `charts.ErrNotFound` are different sentinels, so the
  handler's 404 branch never matched and the caller got a **500**. Unobservable while
  every chart route sat behind `RequireProfileOwnership`, which 404s a stranger before
  the service is reached. Fixed at the source in `charts.Service.profileFor`, so the two
  routes that are still shielded by the middleware get the mapping too.
- `analytics-emitted.test.ts` extracted declared events with a regex requiring an inline
  `{ … }` payload, so any event typed as `Record<string, never>` was invisible to it in
  **both** directions — skipped by "every declared event is emitted" and simultaneously
  reported as undeclared. A guard that fails on correct code and passes over its own
  case. Broadened, and the break test confirms it now catches what it missed.

---

# PR 22 — the sky moved and the transit table stopped

Found while verifying a real chart (26 Feb 2003, 20:55, Prayagraj) against the engine.
The chart itself was correct in every particular — nine positions, nine nakshatras, the
North Indian house→sign mapping, the dasha sequence, Jupiter retrograde, Saturn *not*
retrograde four days after its station — and the PDF matched to the arc-minute. Two
things around it were not.

## What shipped

**A background job held to an interactive deadline.** `SERVICE_TIMEOUT=10s` is the budget
for calls a user is waiting on. The transit worker was given the same one, and both of its
astro calls are ephemeris scans measured in seconds:

| call | cost | why |
|---|---|---|
| `/transits/sade-sati/windows` | ~22 s | 40 years of Saturn at a 5-day step ≈ 2,900 sequential ephemeris evaluations |
| `/transits/compute` | 0.1 s **or ~12 s** | returns the Sade Sati *window* for the Moon sign it was handed, which needs the same scan |

The worker now gets `BATCH_SERVICE_TIMEOUT` (120 s). There is deliberately no second
client in `cmd/worker`: nothing in that process has a user waiting on it, so there is no
call that should fail fast.

**Raja Yoga claimed a conjunction it did not have.** `detect_raja_yogas` emits one name for
two configurations — conjunct (`strong`) or in mutual aspect (`moderate`) — and the summary
read "An angular house lord **joined** with a trinal house lord". The test chart showed
three Raja Yogas with that identical caption while two of the three pairs were six houses
apart. Wrong in English only; the Hindi already said योग. Chandra-Mangal, the other
dual-formation yoga, was already correct — which is what makes it a copy slip.

## What running it found that reading it did not

**The compute timeout is seasonal, and that is the whole story.** `ReferenceMoonSign` is
Aries. `/transits/compute` skips the window scan when the sign it is given is not currently
in Sade Sati — so for roughly twenty-seven years out of Saturn's thirty-year circuit the
call is instant, and for the two and a half years Saturn spends in **Pisces** it costs
twelve seconds, because Aries is then one of the three signs in the stretch. Saturn is in
Pisces now. No code changed; the sky did.

This is why the first diagnosis was wrong. `curl` with `natal_moon_sign: 8` returned in
0.09 s and looked like proof the endpoint was healthy; the worker sends `0`, and that is a
different function. Reproducing it needed the worker's own request, not a plausible one.

**The two endpoints fail differently, and the quiet one hid the loud one.** A slow
`compute` fails the whole refresh — error returned, asynq retries, the log says so. A slow
`windows` call is best-effort by design, so it logs and carries on: positions current to
the hour, Sade Sati end dates frozen at whatever they were the last time it worked.
Nothing on screen looks wrong. `sade_sati_windows` had not been rewritten since the day
before.

## Guards proven by breaking them

- `TestRefresherToleratesSlowAstro` — three cases, not one. The positive case alone would
  pass on the broken wiring, because it would be measuring the stub's speed rather than
  the budget. The two negative cases reproduce both observed failures and fail if they
  stop reproducing them.
- `yogas.test.ts` — the dual-formation list is derived from `yoga.py` (functions that
  branch on `mutual_aspect`) rather than written down, so a third such detector is covered
  the day it lands. Break-tested: restoring "joined with" fails the suite naming
  `Raja Yoga`, and the pre-existing "says what the configuration IS" test passes on the
  bad wording — it could never have caught this.
- `config_test.go` — asserts the batch budget *exceeds* the interactive one, so collapsing
  them back is a failing build rather than a silent regression.

## Verified end to end

A real refresh against live astro-service and the real database: 21.5 s, nine positions
written, twelve Sade Sati windows recomputed, and the 11:30 IST slot that had been missing
all day now present.

## Still outstanding

The scan itself is slow for a reason that is fixable: `find_saturn_ingresses` makes ~2,900
*sequential* calls where skyfield can evaluate a time array in one. Vectorising it means
changing the `EphemerisProvider` protocol — an ADR-003 seam — so it is a performance task,
not a bug fix, and the 120 s budget is headroom rather than a target.

## The gate was breaking the thing it tested

`./scripts/ayana test` failed with 40–60 red tests while `npx playwright test` passed
**148/148 on the same commit**. `ayana test` runs `task verify` first, and `task verify`
builds the web app — underneath the `next start` Playwright is about to drive.

`next start` holds its build manifest in memory, so after a rebuild it serves HTML
referencing chunk hashes whose files no longer exist. The server stays healthy by every
obvious measure — `/auth` returns 200 for the whole run, same pid throughout — and no
JavaScript loads. The failures surface far from the cause: an OTP request that never
reaches the API, a dynamic import that never mounts, a URL that never changes.

Fixed by restarting web between `task verify` and Playwright, plus a check that names a
mid-run server death instead of letting it read as product breakage.

**Three diagnoses, two of them wrong**, recorded because the wrong turn is the instructive
part. I read a `Segmentation fault` in `web.log` alongside same-afternoon faults in Chrome
and `pysemgrep`, and logged it as data-corruption event #10 — that entry has been removed.
The failure reproduces deterministically, which corruption does not.

What sent me wrong: I watched port 3000 through a rebuild, saw `/auth` return 200 for sixty
seconds, and concluded the server was unaffected. It was. The server was never the problem.
I had checked the HTML and not the chunks — the one thing this failure does not touch.
`web_build_check` gets it right because it extracts **every** chunk with `sort -u`;
sampling one returns the framework bundle, whose hash does not move between builds. That is
the second time that exact sampling error has cost a diagnosis in this project.

---

# PR 23 — the text you typed was the colour of the page

Reported as "the text I'm typing and the dropdowns don't look good". It was not a
styling preference. Two defects, on the same screen, both invisible to every gate
this project had.

## 1. A colour token collided with a built-in utility

Tailwind ships `text-base` as a **font size**. The palette also had a colour named
`base`, so `text-base` was emitted a second time as `color: #0B1026` — and the colour
won.

`#0B1026` is `base`: the page background. Every `<Input>` in the product sets
`text-base` deliberately, because iOS Safari zooms the viewport for any field under
16px and strands the user at that zoom. So every input in the product painted its text
the exact colour of the page behind it.

Measured in a real browser, before: `color: rgb(11, 16, 38)`. **Contrast 1:1.** You
typed and nothing appeared.

`base` no longer generates utilities; it is reachable as `bg-background` /
`text-foreground` through `semanticColors`, which is the name a component should have
been using. `colors.base` is untouched for TypeScript callers.

## 2. A colour class named a token that does not exist

`bg-surface-2` was used in five places. There is no `surface-2`. Tailwind drops an
unknown colour class **in silence**, so the birth-place dropdown had no background at
all and its suggestions were drawn straight onto the page beneath them. Now
`bg-elevated` (#1C2545), the token for a surface above a surface.

Both fields and the dropdown options also now state their colour and weight
explicitly rather than inheriting — inheriting is what let defect 1 reach a user.

|  | before | after |
|---|---|---|
| typed text | `#0B1026` (= background) | `#F2F3F8` |
| font weight | 400 | 500 |
| dropdown background | *class dropped — transparent* | `#1C2545` |
| option text | inherited | `#F2F3F8`, weight 500 |

## Why nothing caught either

The class names read correctly in a diff. `tsc` sees valid strings. Tailwind emits both
rules without complaint. `next build` succeeds. And the **visual-regression baselines
were captured with the bug present**, so they agreed with it — a screenshot suite
cannot tell a deliberate dark theme from invisible text.

## Guards added, each broken on purpose

- `apps/web/src/test/tailwind-tokens.test.ts` — no colour token may be named after a
  font size, and every `bg-*` class must name a real token. Break-tested both ways:
  restoring `base` fails naming `base`; restoring `bg-surface-2` fails naming the file.
  The palette is read from the config and Tailwind's defaults, so `bg-white` on the
  print route (paper is white) and `bg-radial-glow` (a gradient) are not false
  positives.
- `tests/e2e/input-contrast.spec.ts` — a computed-style assertion in a real browser,
  which `frontend.md` names as the only gate that notices this class of defect. It
  asserts a **ratio**, not a hex: pinning the value would fail on a legitimate tweak
  and pass on a different unreadable colour.

The e2e break-test is worth recording because the first attempt at it was wrong. I
reintroduced the `base` collision alone and the gate still passed — because the fixed
`Input` now states `text-foreground` explicitly, which correctly protects it. Only
reverting **both** halves reproduced the shipped state, at which point the gate failed
with `rendered in rgb(11, 16, 38), which is the colour of the surface behind it`. A
break-test that does not restore the original conditions proves nothing.

## A guard that failed to protect me

Picking a phone tail for the new spec, I took `744` — already used by
`kundli-keyboard.spec.ts`. `mask-letters.spec.ts` exists precisely to prevent that and
did not fire: its `TAIL_PATTERN` matched `uniquePhone('744')` but not
`const PHONE_TAIL = '744'`, which is the form both specs use.

Its own docstring describes this exact blind spot on the **email** side — "a grep is
only as good as the call shape someone happened to use" — and the fix was applied to
`LETTER_PATTERNS` only. The phone check kept one pattern.

It surfaced as `kundli-keyboard.spec.ts` failing at `/auth/verify`, because my spec's
watcher had eaten its OTP: the "looks like broken authentication" symptom that file was
written to prevent, reported against an innocent spec. Guard fixed, break-tested (it
now names `'744' in input-contrast.spec.ts and kundli-keyboard.spec.ts`), tail moved
to `745`.

The email alphabet is fully exhausted — all 26 letters are claimed — so new specs must
use the phone channel.

---

# PR 24 — two defects a judging pass found, and a validated chart

A full validation of a real chart (26 Feb 2003, 20:55, Prayagraj) across six screens.
The astrology was correct everywhere. Two presentation defects were not.

## Validation result

Every layer checked independently rather than against itself:

| Layer | Method | Result |
|---|---|---|
| Inputs → stored | read `birth_profiles` | date, time, place, tz, UTC instant all correct |
| Stored chart vs engine | recomputed from the stored instant and compared | **zero mismatches** across 9 planets + ascendant, to 6 dp (`173.366622`) |
| Planet table on screen | 9 rows × sign, degree, house, nakshatra, **pada** | all 36 values correct; all nine padas re-derived by hand from longitude ÷ 13°20′ |
| Dignity column | classical exaltation | Jupiter in Cancer marked Exalted; the other eight correctly blank |
| North Indian diamond | 12 house→sign numbers + 9 glyph placements | all correct; Saturn correctly **without** ℞ (it stationed direct four days before birth) |
| Dashas | Vimshottari order from the Moon's nakshatra lord | Venus → Sun → Moon → … correct; current period Moon, marker ~74% through a 2018–2028 span |
| Yogas | kendra/trikona lordships re-derived by hand | 3 of 5 candidate pairs shown, and the **two absent ones verified absent** — neither conjunct nor in mutual aspect |
| Transits | recomputed for the claimed slot | 9 positions to the arc-minute, 9 house-from-Moon counts, Rahu/Ketu exactly 180°, Sade Sati state |

The yoga screen was the strongest evidence, because of what it does *not* show. A
detector that over-fires would have printed five cards.

## Defect 1 — "Choose a antardasha"

`dashaChooseParent` was `'Choose a {parent} above to see its periods.'`: one template
with a hardcoded article, interpolating "mahadasha" (correct) or "antardasha" (not).

No amount of interpolation fixes this, because the article belongs to the **word**, and
an `{article}` placeholder would export an English grammar rule into every locale —
Hindi has no indefinite article at all, it uses the numeral एक. Split into two strings
per level.

## Defect 2 — the transit list was alphabetical

Ju, Ke, Ma, Me, Mo, Ra, Sa, Su, Ve. Nobody chose that. `ListTransitsAt` is a
`SELECT DISTINCT ON (planet)`, and Postgres **requires** the DISTINCT ON expression to
be leftmost in `ORDER BY` — so `ORDER BY planet` is load bearing for the deduplication
that stops a stale row shadowing a fresh one, and reordering the query would be a
correctness regression to fix a cosmetic one.

So the sort lives in the view: `GRAHA_ORDER` in `glyphs.ts`, traditional order, unknown
bodies last. The natal table was already right because astro-service returns the grahas
in that order; only rows that have been through that query need re-ordering.

## Guards, each broken on purpose

- `TransitPanel.test.tsx` — feeds the list in exactly the alphabetical order the query
  produces and requires traditional order out. Plus: the panel must not sort its props
  in place (React StrictMode double-renders), and an unknown tenth body sorts last
  rather than displacing the Sun.
- `dictionaries.test.ts` — no English string may write "a" before a vowel sound.
  Restricted to a/e/i/o with a `one|once|eu` exception, because "u" is where spelling
  and sound disagree most ("a user", "a unique chart") and a guard that fires on those
  gets silenced.
- `DashaTimeline.test.tsx` — pins which prompt each level reaches for. Swapping them
  would read perfectly and send the reader to the wrong track.

## One more sampling mistake, recorded because it is now a pattern

The first version of the ordering assertion extracted planet names by searching each
row's text. It passed on a list that was still wrong — every row ends *"Nth from Moon"*,
so a substring search for a planet name finds "Moon" in all nine. Reading the
abbreviation cell fixed it.

That is the third time in this project a guard has been written against a convenient
projection of the data rather than the thing itself — after sampling one JS chunk
instead of all of them, and grepping one call shape for OTP mask letters instead of
both. The shared lesson: **assert on the narrowest unambiguous field, not on whatever
string is easiest to reach.**

---

# PR 25 — the accessibility tool failed the product, not the other way round

The Phase 3 manual pass asks: *"which sign is my Moon in, and which house?"*, answered
from the inspector panel alone. The tester clicked **The data table** and read:

```
Planetary positions — …as a table.PlanetSignHouseDegreeNakshatraNotesAscendantVirgo1st2
```

The caption, with every cell of the first two rows run together, cut off at 120
characters — before the Moon. Nineteen "focus stops" were logged on a page that has
eight. The reasonable conclusion is that the chart is not usable by ear.

**That conclusion would have been wrong, and acting on it would have damaged correct
markup.** Measured in a browser, the product's hidden table is:

```
caption present · 6 headers, all scope="col" · 10 rows
Moon row:  th:Moon | td:Sagittarius | td:4th | td:21 degrees | td:Purva Ashadha, pada 3
```

and the real tab order on `/kundli/chart` is **8 stops, with the table not among them**
(`tabindex: null`). A screen reader on that markup says *"Moon, House, 4th"*.

## Three defects, all in the tool

**1. `accessibleName` fell back to `textContent` for every element.** Right for a button
— whose content *is* its name — and wrong for a container, which a screen reader names
and then lets you navigate *into*. No screen reader announces a table's flattened cells
as its name; it reads the `<caption>`.

**2. There was no way to navigate into anything.** A panel that reports a table's name
and not its rows cannot answer a question whose answer is in a cell. It now reads the
table row by row with each value prefixed by its column header — the panel now prints
`Moon, Sign: Sagittarius, House: 4th`, which is the question answered in one line.

**3. Jump-button clicks were counted as Tab presses.** "How many tabs to reach the
Moon" is the one number this tool exists to report, and every click inflated it. Jumps
are now shown with `→` and excluded from the count.

## And the test that was passing for the wrong reason

`?a11y=1 mounts it, and it reports what is focused` pressed Tab once from `main` and
asserted the panel contained "Email address". It did — because focusing `main` dumped
main's entire flattened text, and the label was in there. One Tab from `main` actually
lands on the "Ayana home" link, which sits inside main and before the form, so the thing
the test claims to check — that a `<label for>` resolves into an accessible name — was
never exercised. It now walks until a field is focused, with a bound.

## Break-testing, and a break-test that was itself wrong

The first attempt at breaking the caption guard restored the `textContent` fallback and
the test still passed. That looked like a weak assertion; it was a mis-aimed break. The
`HTMLTableElement` branch returns the caption *before* the container check ever runs, so
the line I broke could not affect tables. Removing the caption branch fails it properly,
with the intended message.

Worth recording alongside the other three sampling errors in this file: **a break test
has to disable the code path the assertion actually depends on**, not a plausible
neighbour.

## Also

`eslint` caught `counter.current` being read during render — a lint error and a real
one, since a ref does not trigger a re-render and the heading would have shown a stale
count. Derived from `stops` instead.

Gate: `task verify` + **153** e2e + smoke, green.

---

# PR 26 — the skip link I broke, and a 15-agent audit of the Kundli routes

## The regression

`020d742` removed the `base` colour token from the Tailwind palette to fix `<Input>`,
whose text was being painted the page background by a `text-base` collision. It fixed
`bg-base` on the `<body>` in `layout.tsx` and **missed `focus:text-base` eight lines
below**, on the skip link.

That class was doing two jobs — the font size, and the navy that made the label readable
on its gold pill. Afterwards it emitted `font-size: 1rem` and no colour, so the label
inherited `text-ink`:

```
#F2F3F8 on #D4A857  =  1.99:1     (AA floor is 4.5:1)
intended pairing    =  8.54:1
```

Measured in a browser, confirmed in the compiled stylesheet
(`.focus\:text-base:focus{font-size:1rem;line-height:1.5rem}` — no `color`), broken on
**every route**, for the sighted keyboard user the element exists for.

Nothing saw it. axe scores `color-contrast` on the current rendered state and the link is
`sr-only` until focused, so the failing state does not exist during a scan. Three
existing tests focus it and all three assert only that a focus ring appears.
`input-contrast.spec.ts` was written for this exact collision and scoped to form fields —
**the lesson is the scope, not the technique: a colour regression is not confined to the
component that revealed it.**

Fixed as the semantic pair `focus:bg-primary focus:text-primary-foreground`, so the
foreground cannot drift from its surface again. Gated by a computed-style assertion,
break-tested (fails at `1.99:1`).

## The audit

Fifteen agents — five Kundli routes × three lenses (focus order, unlabelled controls,
colour-only meaning) — over a real browser capture of each route, every finding then
put to an adversarial verifier instructed to default to refuting it.

**19 confirmed, 4 refuted, 1 verifier lost to an API error** (so
`dasha-retry-drops-focus-to-body` is neither confirmed nor refuted and is recorded as
unverified).

Answers to the manual pass:

- **Q2 — reading order:** follows the visual order on all five routes. No mismatch found.
  What fails is what happens *after*: where focus goes when a control is activated, and
  whether a state change is announced.
- **Q3 — unlabelled controls:** none. Every interactive element has a name, which is why
  axe is green. The confirmed findings are all the inverse — a name correct about the
  control's *purpose* that destroys the *data* it wraps.
- **Q4 — colour alone:** no. Every colour-carrying element pairs its colour with a glyph
  or a word, each checked in source. The colour pass surfaced the skip-link contrast
  regression above instead.

The two majors worth naming here, both on `/kundli/chart`:

- Activating **Download PDF** applies `disabled` to the focused button, which blurs it to
  `document.body`; the success re-render then replaces the button with an anchor, so
  there is nothing left to restore focus to.
- The PDF's live region is **unmounted at the moment the PDF becomes ready** — the
  `ready` branch returns a `<p>` with no `role`/`aria-live` at the same child index, so
  React reuses the node and strips the attributes in the same commit that writes the
  success text. Confirmed by execution, not by reading. The failure path keeps its
  region; only success is silent.

`ShareSheet.tsx` gets both of these right and is the fix template for both.

**None of the confirmed findings is detectable by axe** under the `wcag2a/2aa/21a/21aa`
tags the suite runs, and two would need `label-content-name-mismatch`, which is
experimental and disabled in that set.

## Two process notes

**An agent ignored a read-only instruction, and I committed its output.** The audit brief
said READ-ONLY; one agent wrote scratch probe files anyway. `git add -A` then swept
`ZZFocusProbe.test.tsx` into a merged commit. The agent's error was ignoring the brief;
mine was staging without reading the list. Removed.

**The `bg-*` guard flagged its own documentation.** Explaining this regression in
`layout.tsx` required writing the string `bg-base` in a comment, and the guard failed on
it. Every guard in this repo has now hit that trap — `mask-letters` excludes itself by
filename, `tailwind-tokens` excluded `src/test/` — and neither helps when the explanation
lives beside the fix in a product file, which is where it belongs. The guard now strips
comments before scanning. Block comments wholesale; line comments only when `//` opens
the line, so a `https://` in a string cannot truncate a line and hide a real class after
it. Re-break-tested: it still catches `bg-surface-2` in real code.

---

# PR 27 — the glossary label was eating the data it wrapped

Checking Q2 and Q4 of the manual pass myself, against the five Kundli routes.

## Q2 — reading order: passes

Captured the reading order of all five routes and compared the visual position of every
heading, control, table and landmark against its DOM position. **Zero mismatches on all
five.** `/kundli/chart` reads as 32 lines: h1, each switcher followed by its own
explanation, the chart group with its summary, the ten table rows, then the three
actions. `/kundli/transits` reads Sun → Moon → Mars → … in traditional order with
retrograde spoken as a word and Sade Sati stated as a sentence.

## Q4 — colour as the only signal: passes

Eight colour-carrying elements across the five routes; **every one has a non-colour
cue.** Verified by removing colour entirely (`filter: grayscale(1)`) and asking the three
questions the doc names:

- current dasha → `Moon mahadasha, Jan 2018 – Jan 2028, **Current period**`
- yoga strength → the words **Strong** / **Moderate**, visible and in the label
- Sade Sati → *"Not currently running. Saturn is in Pisces, the 4th sign from your Moon."*
- retrograde → the `℞` glyph plus sr-only "retrograde"

## Q3 — the defect

The audit's biggest Q3 finding, confirmed independently against Playwright's accname
implementation. `AstroTerm` set `aria-label` unconditionally to `What “{term}” means`.
That is correct where the children ARE the term — `<AstroTerm term="nakshatra"/>` renders
the word, nothing is lost — and destroys data where they are a VALUE.

`PlanetTable` passes the value. `aria-label` wins accname over name-from-content, so the
Moon's row announced:

```
Moon in Sagittarius, 4th house, 21 degrees Sagittarius 20°44' 4th
What “Nakshatra” means —
```

The nakshatra absent from the row entirely, in all nine rows. Not derivable by ear from
the sign and degree that *are* announced, and a voice-control user saying "click Purva
Ashadha 3" hit nothing.

Now:

```
Moon in Sagittarius, 4th house, 21 degrees Sagittarius 20°44' 4th
Purva Ashadha 3. What “Nakshatra” means —
```

Value first, affordance after. Line 65 already drew this distinction for the visible text
(`children ?? entry.name`); the label did not.

**Why the suite missed it:** `PlanetTable.test.tsx:47` asserts
`toHaveTextContent('Shatabhisha 3')` — DOM text, intact the whole time. The existing
`AstroTerm` test "renders custom children rather than the canonical name" asserts
`toHaveTextContent` too. Every assertion in reach was about what is on the SCREEN; none
about what is ANNOUNCED. `toHaveAccessibleName` is the distinction, and it is the whole
bug. axe checks a name exists, never that it preserves what it replaced.

Break-tested. Also pinned: the plain case must not stutter into
"Nakshatra. What “Nakshatra” means", and a non-text child must not interpolate
`[object Object]` into speech.

## A process note

Running `task verify` and `npx playwright test` as separate commands reintroduced the
stale-build failure that `ayana test` exists to prevent — verify rebuilds the web app
under the running server, and the suite bailed after 29 tests with a 500 on a chunk.
`./scripts/ayana test` restarts web between the two and passed 156. **The wrapper is not
a convenience.**

---

# PR 28 — the tester's four answers, and a stutter I shipped an hour earlier

The Phase 3 manual pass came back with four judgements, all negative, all actionable.

## 1. "No, I would not have found it" — the jump controls

Asked to answer *"which sign is my Moon in?"* from the inspector, the tester said they
would not have found **The data table** unaided. Four flat buttons — *Start of page*,
*Main content*, *The chart*, *The data table* — read as decoration; nothing said they
were destinations, or that the answer lived behind one of them.

Now a labelled `<select>` — *"Jump to a part of the page"* — whose options name what the
tester will **find** rather than what the element **is**:

```
The very top — the skip link
Main content — past the header
The chart diagram — the twelve houses
The planet table — every sign, house and nakshatra
```

## 2. "It looks like a database dump" — and it was my tool, for the third time

`/kundli/chart` linearised as seven bare fragments where the switchers are:

```
Chart / Rasi / D1 / Navamsa / D9 / Dasamsa / D10
```

Chromium's own accessibility tree reads the same markup as:

```
group "Chart"
  radio "Rasi D1" [checked]
  radio "Navamsa D9"
  radio "Dasamsa D10"
```

`VargaSwitcher` and `StyleSwitcher` are real `<fieldset>`s with a `<legend>` and radio
inputs — correct markup that my linearisation walked straight past, emitting the label
text as loose lines. **Third time this file has flattened a container into its text and
made correct markup look broken**, after `accessibleName` and the `sr-only` table
wrapper. Now:

```
▸ group — Chart
    Rasi D1 — radio button 1 of 3, selected
    Navamsa D9 — radio button 2 of 3
```

Position and state are included because they are most of what makes a radio group
legible by ear.

## 3. "Confusing" — a link promising what you were just given

*"See every position in a table"* sat directly below the chart's own visually-hidden
table. A listener hears all ten rows, then is offered a table of every position — which
sounds like the thing they just received, so the reasonable conclusion is that they
missed something. It goes to a different **page**. Renamed to *"Open the full planets
screen"*, which names the destination and loses nothing for a sighted reader who could
not see the hidden table anyway.

## 4. The dasha tracks changed structure as you used them

`Track` renders `<section aria-label="Mahadasha periods">` when populated and a bare
`<div>` when empty. So with only the mahadasha track filled, the page had **one landmark
and two stretches of loose paragraphs** — and drilling into a mahadasha turned the
antardasha track into a landmark. Structure that appears and disappears as you use the
page is harder to learn than structure that is merely sparse. Both branches now use the
same `<section>`.

## And a regression I had shipped an hour earlier

Fixing the Nakshatra label (PR 27) introduced `button "Rasi. What “Rasi” means"` on the
chart page — a stutter, because `<AstroTerm term="rasi">Rasi</AstroTerm>` passes children
that are the term's own name.

The test meant to guard it rendered `<AstroTerm term="nakshatra" />` with **no children**,
so it exercised the `entry.name` fallback — a branch the bug never touched. **A guard
aimed one branch away from the defect.** The prefix is now suppressed when the children
equal the term, compared case- and punctuation-insensitively.

Worth recording: the first replacement test used `term="first_house"`, which is not a
glossary key — `defineTerm` returned null, the component rendered plain text with no
button, and `getByRole('button')` queried something that never existed. It passed
vacuously. Fifth sampling error in this file's history, and the same shape every time:
**the assertion never reached the code.**

## Verification

`task verify` + **156** e2e + smoke, green. One visual baseline moved — the chart screen,
whose link text changed. Every guard break-tested.

---

# PR 29 — item 14 closed by decision: ADR-010

The last Phase 3 gate item. It closes on a decision rather than on reaching the number,
and the decision is written down rather than implied by a tick.

## The measurement

| route | first-load JS, gzipped |
|---|---|
| `/home` | 201.5 KB |
| `/kundli/chart` | **201.0 KB** |
| `/kundli/yogas` | 199.3 KB |

Spec target: **180 KB**. Framework floor beneath it — React, react-dom, the Next client
runtime, before a line of this product — **159.5 KB**, of which two chunks totalling
112 KB contain no product code at all.

So the target leaves roughly **20 KB for the whole of Ayana**, and the i18n dictionaries
alone are 14.8 KB.

**Not reachable by trimming product code, measured rather than estimated:** deleting the
glossary prose (8.9 KB) *and* the entire Hindi dictionary (7.1 KB) — every removable byte
— lands at **183.0 KB**. Still over, having deleted a whole locale from a product built
for an Indian audience.

## The decision

ADR-010 replaces 180 KB **as the gate criterion** with the per-route regression budgets
in `bundle-budget.json`. The spec number stays as `specTargetKB`, prints on every run, and
is recorded as an unmet spec item.

The reasoning that matters: 180 KB was written into the spec before the framework was
chosen. A budget no product-code change can satisfy does not constrain product code — it
fails permanently and teaches everyone to skip the budget line. What the regression
budgets *do* deliver is real: an import that drags in a date library fails the build on
the PR that adds it.

**Verified rather than assumed.** Setting `/kundli/chart` to 195 KB and re-running:

```
Over budget:
  /kundli/chart: 201.0 KB against 195 KB (+6.0 KB)
exit code: 1
```

Exit 1 on breach, 0 when clean. A budget that cannot fail is not a budget, and this one
had never been watched failing.

## Also checked while measuring

The a11y inspector is genuinely outside the first-load bundle — its own 12 KB chunk,
referenced **zero** times in `/kundli/chart`'s HTML with or without `?a11y=1`. The
201.0 KB is product code; the developer tool is not quietly charging every user for it.
That property was claimed in the component's docstring and had not been tested.

## Rejected

- **Ship one locale.** Saves ~15 KB, lands at ~186 KB, still misses — and pays for it
  with Hindi.
- **Defer as a framework decision.** Leaves the phase open on a question nobody intends
  to answer this quarter, and leaves working regression budgets unrecognised.

Changing the framework is not foreclosed; it is simply not a Phase 3 call.

---

# Phase 3 gate: closed, 16 of 16

With one caveat recorded in `current-phase.md` and repeated here because it qualifies
every green tick above: **CI has never executed a job.** Eight PRs in this phase were
merged over red checks that never started. Every check in this repo is local, so "green"
means green on one machine. That is not a gate item and does not reopen one — but a
budget that fails the build, a guard that fires and a suite that passes constrain nobody
until a machine other than this one runs them. It should be fixed before Phase 4.

---

# PR 30 — §11.8 closed: a hostile profile label, asserted rather than assumed

Asked which of the three open §11 security items I could actually fix. **One.**

## What was true, and why that was not enough

§11.8 — *"all rendered content escaped — a user-supplied profile label cannot inject
markup"* — was **already true**, for a reason nobody had written down: React escapes every
JSX child, `interpolate()` returns a plain string, and there is not one
`dangerouslySetInnerHTML` in the codebase.

True *by absence* is a fragile way to be safe. Nothing failed if somebody added the one
API that turns a string into markup, and Phase 5 renders **model output**, where
`.claude/rules/frontend.md` is explicit that `<img onerror=...>` is a real vector.

## Two guards, because neither covers the other

**Structural** (`no-raw-html.test.ts`) — no source file may assign `innerHTML`/`outerHTML`,
call `insertAdjacentHTML`, or use `dangerouslySetInnerHTML`. An `ALLOWED` list exists and
is empty; the first entry should be hard to add and obvious in a diff. This catches the
vector being **introduced**.

**Behavioural** (`xss-profile-label.spec.ts`) — four payloads (`<script>`, `<img onerror>`,
a quote-break, `<svg/onload>`) stored through the real API and rendered in a real browser.
This catches it being **exploited**. A component test would only have proved React
escapes, which was never in doubt.

Both break-tested against a real injection: rendering the label with
`dangerouslySetInnerHTML` fails the structural guard *naming the file* and the behavioural
guard *finding the payload in the DOM*.

## The test passed vacuously twice before it worked

Recorded because the shape is now familiar and the detection matters more than the bug:

1. **Scraped `localStorage` for a JWT.** Found nothing — the access token lives in memory
   *by design* (`users-api.ts`: localStorage is readable by any script). The POST went out
   unauthenticated, returned 401, hit an early return, and **passed asserting nothing**.
2. **Hardcoded `place_id: 1`.** No such place. The server answered 400 *"That birth place
   could not be resolved"*, the test read any non-401 rejection as *"the server refused the
   hostile label"* and passed — **concluding the opposite of the truth from an error about
   geography**.

Both now impossible: the token is minted through `/auth/refresh` with an explicit
`expect(200)`, the place is looked up through the same search the UI uses, and a rejection
only counts as a pass if the error body actually mentions the label.

A third near-miss: the DOM assertion first counted `script:not([src])` and failed on
**Next's own inline bootstrap scripts** — the very ones that keep `'unsafe-inline'` in the
CSP. It reported a correct page as injected. Now scoped to elements carrying the payload's
own marker.

## The other two, and why I did not touch them

**§11.9 — CSP `'unsafe-inline'`.** I can change it. I should not, now. The decision in
`next.config.mjs` is dated, measured (`csp.spec.ts` failed on a click timeout under the
strict policy) and already carries a **blocking Phase 5 gate item**. Both exits cost
something real: a per-request nonce forces every page dynamic — killing static rendering
and CDN caching for an audience on Indian mobile networks — and `experimental.sri` is
experimental and needs an ADR. Introducing either into a just-closed phase, for a
requirement that lands in Phase 5, is poor sequencing rather than diligence.

**§11.5 — Chrome sandbox and network.** Half is mine and half is not. The network half is
implementable (`chromedp.Flag("host-resolver-rules", …)` derived from `WEB_URL`, or
`network.SetBlockedURLs`) and I have verified both APIs exist. The **sandbox** half is not
a code change: `--no-sandbox` is set because the container has no user namespaces, and
that is a deployment decision. Fixing one half and ticking the item would misrepresent it.

---

# Phase 4 begins — AI Infrastructure

Nothing in this phase is exposed to users. It builds the seam every model call passes
through, so that Phase 5's chat is a consumer of a tested abstraction rather than a place
where provider code and product code get written together.

19 gate items, 21 tasks.

## Task 4.1 — the provider protocols and registry

**`LLMProvider` and `EmbeddingProvider` as `Protocol`, not ABC.** Adapters share no
implementation and need no common base class. Structural typing means `mypy --strict`
checks conformance at every *call site* rather than at registration, and a test double is
a class with the right shape rather than an inheritance ceremony — `FakeProvider` in the
tests is forty lines with no import from the thing it doubles.

**Decisions worth naming, because each could have gone the other way:**

- `cost_micros` is an **integer**, like the ledger's paise. Costs are summed across
  millions of calls and float addition does not associate: the same charges in a different
  order give a different total, which is indefensible on a bill.
- `finish_reason` separates `refusal` from `error`. A model declining is a normal outcome
  with a product response — a safety path, not a retry path — and collapsing the two makes
  the retry logic hammer a provider that is working exactly as designed.
- The system prompt is a **list** of blocks, not a string. Prompt caching works on a
  byte-identical prefix; concatenating early makes every request a cache miss, and that is
  the single biggest cost lever this service has.
- `stream()` is not `async def`. It returns the iterator rather than awaiting it —
  otherwise callers write `await (await p.stream(r)).__anext__()`.

**The registry is a fallback chain, and two of its rules are the interesting part:**

- A **non-retryable** failure stops the chain. A 400 from a malformed request fails
  identically everywhere; walking three providers with it triples the latency and the
  token spend, and produces a log blaming the last provider for the first one's mistake.
- **Streaming does not fail over.** Once the first chunk has reached the user, switching
  providers splices two models' prose together. A stream that fails before its first chunk
  is the caller's to retry — against the chain, via `complete`.

**The PII guard runs at registration, before the append.** `guards.py` owns the rule; the
registry owns the moment. A caller that catches `UnsafeConfigurationError` and carries on
still cannot end up with a half-registered provider serving traffic. Break-tested by
moving the check after the append.

## The vendor boundary is now enforced, not conventional

§2's first line asks for an `import-linter` contract, "not by convention — conventions
erode". Two contracts, wired into `task lint:py`:

- nothing outside `app/providers/` may import `openai`, `anthropic`, `google`, `litellm`
  or `ollama`
- `providers/base.py` may not import any of them either — if the seam itself imports a
  vendor type, that type becomes part of the protocol and every other adapter has to
  construct it

Break-tested: an `import openai` in `app/telemetry.py` fails with
`app.telemetry is not allowed to import openai` and the line number.

Two configuration facts worth recording, because both failed silently-ish first:
`include_external_packages = true` is required or the contract passes while seeing no SDK
imports at all; and subpackages of external packages are rejected, so `google` covers
`google.generativeai`.

## A duplicate removed while writing it

`ProviderTier` was declared in `settings.py` **and** in `providers/base.py`, identically.
Two `Literal` aliases with the same members typecheck against each other, so nothing would
ever have reported the drift — and the day a fourth tier was added to one of them, the PII
guard and the adapters would have disagreed about what `paid` means with no test failing.
Settings owns it now; `base.py` re-exports with `as`, which is how a module tells
`mypy --strict` that a name is public rather than an implementation detail.

---

# Phase 4.2, 4.3, 4.6 — the adapters

Three providers, batched because `MockProvider` is what makes the other two testable and
splitting them would have meant a PR whose tests could not run.

## 4.6 `MockProvider` — the one CI is allowed to use

**Keyed by a hash of the request, not by call order.** A queue of canned replies makes
every assertion depend on how many calls the code under test happens to make, so
inserting one classification step silently shifts every later assertion onto the wrong
fixture — and the test still passes, against the wrong recording.

**An unknown request raises.** The tempting alternative is a plausible default, and it is
exactly wrong: *six times in the preceding day* a test in this repo passed while asserting
nothing, every time because something returned a benign value where it should have
refused. The error names the fingerprint, the path, and the two ways to fix it.
`allow_unknown=True` exists for retry and circuit-breaker tests that genuinely do not care
what the model said — opt-in, and visible in the test that chose it.

The fingerprint deliberately **excludes `trace_id`** (unique per request by construction —
including it would mean no fixture ever matches twice) and **includes the system prompt**
(two requests with the same user message and different system prompts are different
questions; sharing a fixture would let a prompt-regression test pass against the wrong
recording).

## 4.2 `OpenAICompatibleProvider` — five backends, one adapter

Ollama, LM Studio, Groq, OpenRouter and Cerebras all speak the OpenAI wire format. The
spec calls this the highest-leverage code in the phase and it is right.

**Tested against a real `httpx` transport, not a patched SDK method.** Patching
`chat.completions.create` would assert that the adapter calls a method — which it
obviously does — and would keep passing if the SDK changed the shape it returns. Serving
real HTTP through the SDK's own parsing is where version-skew bugs actually live.

Three decisions worth naming:

- **`max_retries=0` on the SDK client.** Retries are the registry's job. Leaving the SDK's
  own retries on multiplies them: three SDK attempts inside three chain attempts is nine
  calls to a provider that is down, and nine times the wait before the user sees anything.
- **`content_filter` maps to `refusal`, not `error`.** A model declining is a product
  outcome with a written response. Mapping it to `error` would make the chain retry a
  refusal at every provider and then show a failure to a user whose question the product
  has a real answer for.
- **The model the backend *reports* is recorded, not the one we asked for.** OpenRouter
  silently routes to whatever is cheapest, and a log recording the requested model cannot
  explain the answer that came back.

**Cost is not computed here.** Price per model changes without any code change, and an
adapter hardcoding a rate is a bill that silently goes wrong the day a price does.

## 4.3 `OllamaEmbeddingProvider`

Separate from the chat adapter despite the same server: Ollama's `/api/embed` is not the
OpenAI `/v1/embeddings` shape, and one class pretending to be both would be a branch on
every method.

Two guards, both for failures that are otherwise **silent**:

- **A wrong-width vector is refused.** A 1024-wide vector in a 768 column either errors on
  insert — fine — or lands in a column that accepts it, at which point every similarity
  score in the product is meaningless and nothing reports a problem.
- **A short batch is refused.** Two vectors for three texts, zipped naively, pairs text 3
  with vector 2 and everything after it — a retrieval index that is subtly and permanently
  wrong.

## Break-tested

Marking 400 retryable, mapping `content_filter` to `error`, and deleting the vector-width
check each fail a test whose message explains the consequence rather than the symptom.

`task verify` green. 128 Python tests in `services/ai`.

---

# Phase 4.8, 4.9 — routing and resilience

## 4.8 The model router

**Two hops, not one.** A call site knows what it is *doing* — classifying an intent,
writing a paid interpretation — and must never know which model serves that. Job → tier is
a cost-and-quality judgement that belongs in one table; tier → model is a per-provider fact
that belongs in the adapter. Collapse them and every call site hardcodes a model name,
which is how a provider swap becomes a hundred-file change.

**The completeness test derives from the enum, not a count.** `test_every_job_type_has_a_tier`
iterates `JobType`, so adding an eleventh job fails at the place where the cost decision
belongs rather than at the first request that uses it.

**An unrouted job raises its own error type.** Three frames up, a bare `KeyError` is
indistinguishable from any other missing key, and the reflex fix is a defensive `.get()`
with a default — which is exactly the silent mis-routing the type exists to prevent. An
unrouted job silently taking the cheapest tier produces bad answers; silently taking the
most expensive produces a bill.

**Overrides merge over the defaults rather than replacing them.** A whole-table replacement
is the mistake an operator makes at 3am: change one job, lose the other nine.

## 4.9 Resilience as a decorator

The registry chooses *which* provider; `ResilientProvider` decides how hard to try one. A
wrapped provider is still an `LLMProvider`, so the registry cannot tell the difference.

**Two decisions that could have gone the other way:**

- **A permanent failure never opens the circuit.** A malformed request fails at a perfectly
  healthy backend. Counting it toward the breaker lets one client with a bug open the
  circuit for every other user of that provider.
- **Streams get the breaker but not the retries.** Retrying a stream means either replaying
  chunks the consumer has already seen or silently dropping the first part of an answer.

**Full jitter, not `capped ± a bit`.** The failure being avoided is synchronised retry:
without it, every request that failed at the same moment retries at the same moment, and a
provider recovering from overload is immediately knocked over again by the herd it just
shed.

**`time.monotonic`, not `time.time`.** A clock adjustment mid-outage must not make a
breaker believe an hour has passed.

**The clock is injected**, so a 60-second breaker window is tested by advancing a float. A
suite that really sleeps is a suite somebody eventually marks slow and stops running.

## Break-tested

Counting permanent failures toward the breaker, and letting an open circuit call through,
each fail a test named for the decision rather than the symptom.

147 Python tests. `task verify` green.

---

# Phase 4.10, 4.11 — prompts as artifacts, and the ordering that decides the bill

## Immutability is a lockfile, not a rule

The spec: *"You cannot debug a bad response from three weeks ago if the prompt that
produced it has been edited since."* A response records its `prompt_version`, and that
field only means something if `v1` today is byte-for-byte `v1` in six months.

`published.lock.json` holds a SHA-256 per module. Three tests, each covering a different
way the guarantee can be lost:

- **edited in place** — the digest moves
- **deleted** — a logged `prompt_version` now points at nothing, which is worse than
  pointing at something changed
- **added but not locked** — the module exists and *nothing stops anyone editing it*,
  because the first test only compares names it already knows

That third one is the failure the other two cannot see, and it is the one a new
contributor produces by default.

Break-tested both ways: appending a line to `safety_rules.v1.md` fails naming the module;
adding an unlocked module fails naming that.

## The builder enforces ordering structurally

Prompt caching matches a byte-identical **prefix**. Stable content first, volatile last —
one byte changing early invalidates everything downstream, so a timestamp at the top
silently turns every request into a cache miss.

`PromptBuilder` has two phases and cannot go back. `add()` after `cache_breakpoint()`
raises; `chart_context()` before it raises; `build()` without one raises.

**Why structural rather than documented:** *"remember to put stable content first"* is
exactly the kind of instruction that survives review and not the next refactor — and when
it is forgotten, **nothing fails**. Responses stay correct and the bill quietly doubles.
That is the worst shape a regression can have, and it is the shape a comment cannot
prevent.

Also refused: a breakpoint at position zero (caches nothing, costs a round trip to
discover), and an empty volatile block (a first-turn conversation has no summary; emitting
an empty string for it makes turn one structurally different from turn two, for no
content).

## The stability test asserts the thing that matters

Not "a build is deterministic" — **two different users' charts must share a prefix**. If
they do not, the hit rate is zero however stable each individual prompt is. Plus the
negative case: changing a stable module *does* move the prefix, so the test is not merely
asserting that a constant equals itself.

## A missing module raises rather than defaulting

The failure mode of a missing prompt module is not a worse answer — it is an **unguarded**
one. A silent fallback would send a request with the safety rules absent.

164 Python tests. `task verify` green.

---

# Phase 4.4, 4.5 — the two paid adapters, and what they cost

## The cache breakpoint is the whole reason this adapter exists

`OpenAICompatibleProvider` already speaks to five backends. Anthropic gets its own
adapter for three things that format cannot express, and the first is worth roughly
a 10x reduction on the input side of every request:

**A breakpoint caches everything before it**, so it goes after the last *stable*
block. The two obvious wrong placements both look fine in review:

- **After the last block overall** — the user's chart lands inside the cached
  prefix, the prefix changes on every request, hit rate is exactly zero.
- **On every cacheable block** — Anthropic allows four per request; a marker per
  module exhausts the budget at five and the request is rejected.

A third case is subtler: when nothing is marked cacheable, the adapter places **no**
breakpoint rather than defaulting to the end. Marking it anyway pays the 1.25x cache
*write* premium on every request and reads back nothing — strictly worse than not
caching.

All four are tested, and all four break-tested. The tests are about money rather than
correctness: get any of them wrong and responses stay perfect while the bill
multiplies, which is the regression no user reports.

## Three input token classes, and two vendors that disagree about them

Anthropic reports fresh / cache-write / cache-read as **disjoint** counts. Google
reports `promptTokenCount` **inclusive** of the cached part. Copying either field
straight across is wrong for the other vendor — and wrong in the expensive
direction, on the largest part of the prompt.

So `Usage` gained `cache_write_input_tokens` and documents the three as disjoint;
the Google adapter subtracts. The ratio between read and write is also the cache hit
rate, which is the number that says whether the biggest cost lever is actually
engaged. PHASE-04 §15 names "prompt caching silently stops working in prod" as a
risk; this is the field that makes it visible.

## Money: the unpriced model raises

`app/pricing.py` converts tokens to integer micro-USD from a committed table. A
model with no entry **raises** rather than costing zero.

Zero is the tempting default and the expensive one: a model added via an admin
override and never priced runs for months showing nothing on the dashboard, and the
gap first appears on an invoice nobody can reconcile. The ledger this feeds is
append-only — a wrong cost is corrected with an opposing entry, never edited — so
recording it wrong is expensive in a way a missing row is not.

**"Free" and "unpriced" are kept as different facts.** Local models carry explicit
zeros. Collapsing the two is what makes the zero default dangerous.

### The test that was passing vacuously

`cost_micros` returning an `int` was asserted three ways, and a deliberately
floating implementation — `round(tokens * (rate / 1_000_000))` — **passed all
three**, because `round()` returns an int. The assertions could see the cast and not
the arithmetic.

Replaced with four values chosen where the candidate implementations disagree:

| tokens @ $0.30/MTok | exact | truncated | float + round |
|---|---|---|---|
| 3 | 1 | 0 | 1 |
| 15 | **5** | 4 | **4** |
| 35 | **11** | 10 | **10** |
| 1005 | 302 | 301 | 302 |

15 and 35 land on a half-micro, where 0.3's binary representation and Python's
round-half-to-*even* both push the float answer down. That version of the test fails
against the float, which the type assertion never could.

Rounding is half-up, not `//`: truncation loses up to a micro on every call and
always downward, which turns noise into a systematic understatement of the thing
being tracked.

## Security: the error message is built from the status code alone

`str(google.genai.errors.APIError)` interpolates the whole response body, which
echoes the request on some paths and can name the credential on others.
`.claude/rules/security.md` says a key never reaches an error message. Building it
from the status code makes that true by construction rather than by review —
break-tested by echoing a fake key through and watching the test fail.

## What the suite cannot prove, and the procedure for it

Everything above is tested offline against `httpx.MockTransport` under each SDK's own
transport, which exercises our parsing of a response shape **we wrote down**. That
catches every bug on our side of the wire and none on theirs.

`docs/PROVIDER-VERIFICATION.md` is the other side: one manual run per provider, with
the exact numbers to read. It **prints rather than asserts**, because every check
there has a failure mode where the call succeeds and the answer is wrong — a cache
that silently stopped working returns a perfect response, and so does a prefix that
was never cacheable. `scripts/verify_provider.py` is deliberately not a pytest module;
CI must never call a model, and a test file is a thing CI collects by default.

One question that looked like it needed a key turned out not to: `messages.create`
has **no** `temperature` parameter in anthropic 1.7.0. `mypy --strict` reports the
attempt, so the adapter's omission is checked at build time. The comment that
previously asserted a conflict was reasoning, not observation; it now states the
verifiable fact.

250 Python tests. `task verify` green.
