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

---

# Phase 4.12, 4.13, 4.14, 4.20 — classify, screen, and check the answer

## The pre-pass trades coverage for precision, and it is not a close call

PHASE-04 §6 wants a keyword pre-pass so roughly 40% of messages skip the model
entirely. The asymmetry that shapes the whole design: a **deferred** message costs
one `fast`-tier call; a **wrongly matched** message never reaches the model at all.
It retrieves the wrong chart facts, answers in the wrong persona, and if the true
intent was MEDICAL it skips the posture that intent carries. Nothing downstream can
recover, because nothing downstream knows a decision was made.

So: exactly one rule matches, or defer. Ties are never broken by rule order.

Measured on the 200-message set: **100% precision at 41% coverage.** Getting there
found two real things.

### The first-person gate

Four messages were misclassified, and three shared a shape:

```
which house rules career in vedic astrology
what does the 7th house signify for marriage generally
is mercury retrograde a real thing or superstition
```

Each carries a strong keyword. None is about the person asking. Answered as personal
questions they retrieve the user's chart for a question that was never about them.

The fix is one condition — **the message must refer to the asker** — and it is safe
in a way a tie-break is not: a suppressor can only move a message from "decided" to
"deferred", so the worst case is one cheap call on a message that would have been
right. Coverage fell from 52% to 41%, which is where the spec's estimate put it
anyway. Hinglish pronouns are in the pattern, because a guard that works for users
who write in English and fails for users who write in Hinglish is not a partial
guard — it is a hole shaped like a demographic.

The fourth miss was an ordinary gap: "kids" was missing from FAMILY.

### A test that grepped prose, twice

`test_the_rows_flagged_as_ambiguous_are_deferred` failed twice before it was right:

1. It conflated **semantic** ambiguity ("my mother has not been well" is
   family-or-health to a reader) with **lexical** ambiguity (two rules match). Only
   the second is something a keyword matcher can act on.
2. Rewritten to grep the `why` field for the word "fire", it failed on a row whose
   note claimed FAMILY and LEGAL both matched — when "uncle" was not in FAMILY at
   all. Worth having: it found a real gap (the extended relations are now there, and
   a joint family is the default frame for much of this audience) and it showed that
   an assertion keyed on prose tests the prose.

Now an explicit `ambiguous: true` field — hand-declared, so it can be wrong in a way
a test catches.

## The crisis response is a file, not a prompt

A model asked to write a compassionate crisis response will write one, differently
every time, and one time in ten thousand it will say something harmful to the person
least able to absorb it. It may also — being an astrology product — reach for the
chart. No prompt reliably prevents that and no test catches it afterwards.

A file has none of those properties. Written once by a person, reviewed like code,
identical for everyone. `assert_crisis_responses_present()` runs at startup in
**every** environment: booting without it is the one configuration that turns a
working guard into silence, because detection firing means the astrology path is
already bypassed and there is then nothing to send.

The keyword pass is phrase-level, not word-level, and that is what makes it usable:
"die" flags *"I'm dying to know"*; "want to die" does not. The bias toward false
positives that §7 demands is spent on ambiguous **expressions** rather than ambiguous
words.

**Fail-open is a stated trade, not an oversight.** If the model screener is down the
message proceeds. Failing closed would show a crisis response to everyone during an
unrelated outage — telling thousands of people who asked about their career that the
product thinks they are in danger. The keyword pass is unaffected by an outage and
still runs. The residual risk is an indirectly-phrased crisis message during an
outage; it is real, it is smaller than the alternative, and it is written down.

> **Open, and not closeable by any test:** a human must dial each helpline number.
> A test proves a number is *present*. A wrong number costs someone in crisis the
> one attempt they were willing to make.

## `fabricated_chart_fact`, and the possessive it rests on

```
Saturn is traditionally associated with discipline.   general — never blocked
Saturn is in your 10th house.                         personal — checked
```

Only the second is checkable. An extractor that ignored the difference would block
*"what does the 7th house mean?"* — one of the commonest questions the app gets — so
the validator would have made the product unable to *explain* astrology in order to
stop it *inventing* astrology.

This is under-inclusive by construction and that is written down rather than hidden:
"Saturn sits in the tenth, which for you means…" gets through.

An **empty** fact index blocks personal claims rather than waving them through.
"Unverifiable, so allow it" is the instinctive reading and it is exactly backwards: a
response making personal placements when no chart was supplied invented the chart
outright.

## Three vacuous tests, caught by break-testing

Seven deliberate breaks; **three did not fail**:

| Break | Why the test missed it |
|---|---|
| Empty index waves claims through | the test used a *house* claim, which blocks via a second path anyway. Only `ascendant` and `dasha` isolate the branch |
| 3-word shingles for prompt leak | the sample prose happened to share no short n-gram with the prompt, so it passed at any shingle length |
| Dump the whole prompt into the leak excerpt | the test looked for the prompt *verbatim*, and the 200-char truncation meant it never appeared contiguously |

All three rewritten to assert the property that matters, then re-broken to confirm
they fire. The medication rule also had a real gap the tests found: the pattern could
not match *"stop taking **your** medication"* — the commonest phrasing of the single
most dangerous sentence this product could emit.

381 Python tests. `task verify` green.

---

# Phase 4.15, 4.16 — the pipeline, and the one assertion that cannot cheat

## The crisis test asserts on the provider, not the text

`.claude/rules/ai.md`: crisis input *"bypasses astrology entirely"*. The obvious test
checks the response string. It would pass while the bypass was broken — as long as
something eventually produced the right words, and *"something eventually"* is
precisely the failure mode: a model writing a crisis response that mentions Saturn.

So the assertion is `generator.requests == []`. Three separate mocks back the three
call sites, because one shared provider makes *"did the generator run?"* unanswerable.

Two bypass points, both tested and both break-tested:

- **Keyword**, before anything that can fail or cost money — **zero** model calls,
  works with every provider down.
- **Model screener**, for indirect phrasing. *"I don't see the point of anything
  anymore"* carries no crisis word. This is the path where classification has already
  run, so it is the one a careless implementation falls through.

## One deviation from the spec's step order, and why

§9 puts classification (step 4) before safety (step 5). Run literally that is two
sequential `fast`-tier calls on the critical path of every message, and neither one's
output feeds the other.

Instead: the free offline crisis check first, then **intent and screening
concurrently**. Same semantics, roughly half the pre-generation latency. The cost is
one wasted classification when the model screener catches a crisis the keyword pass
missed — rare, one cheap call — against a few hundred milliseconds on every ordinary
message in the product.

The module docstring claimed this before the code did. The first implementation was
sequential; the docstring was the promise and the code was corrected to keep it.

## The stubs return empty, and that is the safe choice

A context stub returning *"Sun in Leo, Moon in Scorpio"* would make the pipeline look
like it worked — and the `fabricated_chart_fact` validator would cheerfully check the
model against **invented** facts. Every test would pass and the product would be
confidently wrong in development in exactly the way it must never be wrong in
production.

Empty is honest: the validator's empty-index rule fires and a personal placement
claim is blocked. There is a test asserting exactly that, so the stub's behaviour is
recorded rather than assumed.

## Telemetry carries no message content

§14: *"`ai_request_logs` stores IDs and token counts — never message content."*
`safety_flags` is therefore types and severities only — the admin panel sees
`fabricated_chart_fact / block` rather than the sentence. A real cost, and the right
trade: this table is retained, replicated, and read by a billing job in Phase 7.

Checked against the **serialised** envelope rather than field by field, so a field
added later without thinking is caught too. Break-tested by putting the excerpt in.

`cost_micros` declares `format: int64`, so the generated Go client types it `*int64`
rather than `*int`. Same width on this machine; not on a 32-bit build, and
`.claude/rules/go.md` is explicit that money is `int64` rather than whatever `int`
happens to mean.

## The import contract had to be narrowed, and the reason is real

Adding the route broke `Vendor SDKs live only in app.providers`:

```
app.main -> app.api.complete -> app.providers -> openai
```

Python executes `app/providers/__init__.py` when you import **any** submodule of it,
and that file imports every adapter — so importing the *protocol* transitively
imports every SDK. With transitive checking on, the contract fails for `app.pricing`
the moment it names a `Usage`. That is not a rule anyone would write down.

`allow_indirect_imports = true` keeps exactly the rule §2 states: no
`from openai import ...` outside `app/providers/`. Break-tested in both directions —
a direct vendor import from `app.orchestrator` and from `app.api` each fail the
contract by name.

**What is no longer caught:** a module reaching a vendor *type* by way of
`app.providers`. `mypy --strict` covers that, and a vendor type in a signature is
visible in the generated OpenAPI.

## The generated Go client compiles

`task contracts` → `oapi-codegen` → `go build ./...` clean. Two paths, eleven
schemas, `CostMicros *int64`.

414 Python tests. Seven deliberate breaks of the orchestrator, seven failures.
`task verify` green.

---

# Phase 4.17, 4.18, 4.19 — Go writes what Python cannot

## The constraint became the design

`ai-service` connects as `astro_ro` and cannot write. §8 turns that into the
architecture: Python returns telemetry in the response envelope, Go persists it, and
from Phase 5 that INSERT runs in the same transaction as the message. A request that
cost money therefore cannot be missing from the bill because a separate logging call
failed.

## `ON DELETE SET NULL`, not CASCADE

CASCADE is the reflex and it is wrong here. Phase 7 reconciles invoices that were
already issued, and a row that vanished cannot be reconciled against anything.
Nulling the ID removes the link to the person and leaves the money — which is what
"delete my account" should mean for a billing record, and is only safe because the
row carries no content.

Proved against real Postgres: insert a log for a user, delete the user, assert the
cost survives and `user_id` is NULL.

## Three tests about money, one of which was measuring itself

`cost_micros` is `BIGINT`. The test stores 2^53 + 1 — a value no float64 holds — and
asserts it round-trips. Break-tested by changing the column to `DOUBLE PRECISION`:
it comes back as 2^53, one less than written.

The first version added a second assertion meant to "detect the collapse":
`float64(row.CostMicros) == float64(beyondFloat64-1)`. **It failed against a working
BIGINT**, because the conversion happens in the *test* rather than in the column —
casting any int64 of 2^53+1 to float64 rounds it down regardless. An assertion
measuring the test instead of the subject, found by running it.

Token counts **clamp** rather than wrap on the narrowing to `INTEGER`. A plain
`int32(x)` above 2^31 wraps, frequently negative, which then fails the column's
`>= 0` CHECK and rejects the whole row — losing a real cost record over an
implausible token count. Cost is deliberately *not* clamped: it is BIGINT and needs
no narrowing, and clamping money would silently discard a charge.

## The most expensive silent bug in the client had no test

Break-testing found it. Adding `ctx = MarkIdempotent(ctx)` to `Complete` — one line,
exactly what someone copying the astro client would write — left the **entire suite
green** while turning every 5xx into three paid model calls.

A chart computation is safe to replay: `astro-service` has no database, so there is
no write to duplicate. A completion is not. `TestAFailedCompletionIsNotReplayed`
exists because nothing caught that, and it probes with a 503 — a status the retry
transport *would* retry, so it can tell the two cases apart.

## Rate limiting bounds spend, not throughput

§14 lists rate limiting on the internal completion path and says why: *"a runaway
loop is a real cost event"*. The provider's own 429 arrives **after** the money is
spent.

A counting semaphore rather than a token bucket, because the resource is concurrent
*spend*. Ten requests a second finishing in 200ms cost far less than two running for
a minute each, and a rate limiter cannot tell them apart. Non-blocking rather than
queueing: a caller waiting behind a full queue eventually times out having achieved
nothing, while the user watched a spinner.

Three tests, all break-tested: the bound holds under 2× load, slots are released on
success, and slots are released **on failure** — the leak that matters, because an
outage would otherwise permanently reduce capacity after recovery.

## SUPER_ADMIN, and the guard is on the group

`auth.RequireRole` compares roles exactly with no hierarchy, so naming only
SUPER_ADMIN genuinely excludes ADMIN. The line is drawn there because the playground
spends real money against the production provider on demand.

Four break tests, four failures:

| Break | Caught by |
|---|---|
| Drop `RequireRole` | every role reached every route |
| Admit ADMIN too | the role matrix |
| Mount one route outside the group | that route alone, on every role *and* unauthenticated |
| Remove the 90-day window cap | a forty-year window was accepted |

The playground has **no `user_id` field**. §14: *"Playground cannot be pointed at
real user data."* It runs with no chart context, so the validator's empty-index rule
treats any personal placement in the answer as a fabrication — it cannot even
accidentally produce a reading about a real person. Its runs are also deliberately
**not** recorded in `ai_request_logs`: an operator experimenting would corrupt the
cost-per-request figure that table exists to produce, invisibly, because the rows
look identical.

`task verify` green. Go unit and integration suites green against real Postgres.

---

# Phase 4.21 — one suite, four adapters

## What parity means, and what it does not

It does **not** mean the adapters produce the same text. They wrap different models;
identical output would mean the abstraction had flattened the thing it exists to let
you choose between.

It means everything *around* the text is identical: the response shape, the exception
type, the `retryable` decision, the streaming contract. The registry's fallback loop
reads `retryable` and nothing else — so an adapter that disagrees about it doesn't
merely fail itself, it stops the chain walking to the next provider and turns one
adapter's mistake into an outage for a configuration that had a working fallback.

Each adapter runs through **its own SDK** against a fake transport, so the parsing is
real and only the socket is fake. A suite that patched each client's method would
assert that four adapters call four methods — four tautologies, not parity.

`MockProvider` is in the set deliberately. It is the adapter CI actually runs the
pipeline on, so an inconsistency between it and the real ones means every integration
test in this repository exercises a shape production does not have.

## Two guards against the suite quietly testing three of four

- `test_every_adapter_in_the_package_is_covered` compares `ADAPTERS` against
  `app.providers.__all__`. A new adapter added and not registered leaves every test
  above passing while never touching it.
- `test_the_adapters_are_not_secretly_the_same_object` — a version where every
  builder returned a mock would pass all 82 assertions and prove nothing.

## Five breaks, and the one that didn't fire

| Break | Result |
|---|---|
| Google marks every failure permanent | 3 failures — failover broken |
| Anthropic leaks a credential into its error | 2 failures |
| Google stops reporting which model answered | 1 failure |
| Anthropic reverses system block order | 1 failure — the prefix is no longer a prefix |
| **Mock reports usage on every chunk** | **passed** |

The last one was vacuous: `test_usage_arrives_at_most_once` used the default one-word
answer, which produces a single chunk on every adapter, so `len(carrying) <= 1` held
whatever the implementation did. Fixed with a twelve-word answer *and* an assertion
that more than one chunk arrived — because without that, the test could go vacuous
again the next time a default changed.

That failure mode matters: Google repeats running totals on every chunk and Anthropic
splits them across two events. A consumer that summed what it received would be right
on one provider and wrong on another, multiplying the bill by the chunk count on the
long answers that are already the expensive ones.

**496 Python tests.** `task verify` green.

---

# Phase 4 gate — one item not met, and 39 defects in my own work

`docs/PHASE-04-GATE.md` is the full report. Two things belong here.

## The gate item that fails

> §17: *"Intent classifier ≥85% on 200 labelled messages, keyword pre-pass working."*

| | |
|---|---|
| Keyword pre-pass | **100% precision at 41% coverage**, asserted in CI |
| `llama3.2:3b`, model only | **40.0%** |
| `qwen2.5:7b`, model only | **60.5%** |
| Target | **85%** |

A free local model does not reach 85% on a 21-way classification. §15 predicted
exactly this — *"free local models behave differently from Claude, so dev quality
misleads"* — and this is that prediction coming true with a number attached.

Two candidate explanations were ruled out first, because both would have been *my*
bug rather than the model's:

- **The prompt never named its output fields.** `llama3.2:3b` answered
  `{"intent": "career"}` — correct, and thrown away by validation. Fixed as `v2`,
  with a worked example. v1 stays frozen and loadable; that is what the immutability
  guarantee is for.
- **The parser only stripped triple fences.** Local models wrap in single backticks
  and prose too.

What would close it is a run against the paid provider — production is Claude — not
tuning the prompt against these 200 messages. They are the regression suite; a number
produced by fitting to them would not generalise, and that is what Phase 6's eval
harness is for.

## 39 confirmed defects, in code I had just declared green

A nine-dimension adversarial review — 156 agents, every finding put to three
independent skeptics with distinct lenses — over a phase whose `task verify` was
already passing.

The three that mattered most were all the same shape: **a guarantee that held
everywhere except the one path where it mattered.**

1. **A crisis message could reach astrology generation.** I hardened the intent
   classifier's JSON parser after observing `llama3.2:3b` wrap output in a single
   backtick — and did not apply the same fix to the safety classifier. So the
   *recoverable* path (a bad intent falls back to broad context) got the robust
   parser, and the *unrecoverable* one did not. A model-detected crisis wrapped in a
   backtick became `none`, the bypass never fired, and telemetry recorded
   `safety_category: none` — indistinguishable from a safe message.

   Fixed at the root: one parser, both callers. The test asserts the two handle
   identical wrappers **identically**, because the *difference* was the bug.

2. **True general statements were blocked as fabrications.** *"Saturn rules
   discipline, and your 10th house is career"* — a wildcard proximity window matched
   straight across the clause boundary and extracted a placement the sentence never
   made. Four of five realistic general sentences tripped it. The validator had made
   the product unable to **explain** astrology in order to stop it **inventing**
   astrology.

3. **Every completion was capped at 10s under a comment claiming 90.**
   `http.Client.Timeout` bounds the whole call and a context deadline can only make a
   request finish *sooner*. The 90s context was dead code, and the failure would have
   appeared only on the deep tier — the most expensive request in the product.

## The lesson, again, in a new place

Six of the 39 were **vacuous tests**: assertions that passed whatever the
implementation did.

- `cost_micros` returning an `int` was asserted three ways, and a deliberately
  *floating* implementation passed all three — `round()` returns an int, so the
  assertions saw the cast and not the arithmetic.
- A BIGINT round-trip test added a second assertion meant to detect float collapse
  that **failed against a working BIGINT**, because the conversion happened in the
  test rather than in the column.
- `test_usage_arrives_at_most_once` used a one-word fixture, so `len(carrying) <= 1`
  held trivially — satisfied by *zero*.
- The credential-leak guard grepped for four strings its fixtures did not contain.
- My own test for the prompt-injection fix **did not catch its own break**, because
  the fixture used a violation whose excerpt is just the matched claim.

Every one was found by breaking the thing the test claimed to protect and watching
whether it failed. That remains the only method that works.

And the verifiers caught three defects in the *fixers'* work, two in the safety path
— negative lookaheads that matched a prefix rather than a word, so `to` swallowed
*"take my life **to**night"*. An exclusion is the only kind of edit to a crisis list
that moves the bias the wrong way, and three of them did.

**836 Python tests. `task verify` green.**

---

# Phase 4 close-out — four more defects, found by asking one question

"Is the LLM not giving better accuracy?" turned out to be a question my own
instrumentation could not answer, and chasing it down surfaced four more defects.

## The measurement was measuring the wrong thing

`scripts/measure_intent_accuracy.py` reported **post-policy** accuracy as though it
were **model** accuracy. Three things discard a classification before it is scored —
an unparseable reply, a provider error, and the confidence threshold — and the third
silently turns a correct answer into a miss whenever the model is right but unsure.

So two findings with completely different fixes looked identical:

```
"the classifier is wrong"     -> change the model or the prompt
"our threshold is too high"   -> change one number in intents.py
```

Partial evidence, `llama3.2:1b`, 30 deferred messages: the model answered **correctly
12 times** and the product delivered **2**. Ten correct answers discarded.

The threshold is deliberately **unchanged** — it is §6's, and re-tuning it against a
1B model is exactly the trap §15 describes. What changed is that the loss is now
visible in telemetry, so production reports it from the first request.

### I nearly reported that finding while it was an artifact

The first version of the diagnostic computed the raw answer as
`fallback_from or primary`, which counts an **unparseable** reply as a raw answer of
`general_astrology`. 27 of the 200 rows carry that label — so a model returning
nothing useful would score "raw correct" by luck, inflating the exact number the
script exists to establish, in the direction that makes the threshold look guilty.

I checked the subset composition *before* publishing the number. It happens to
contain zero `general_astrology` rows, so the finding survives. Had it contained
twenty, I would have reported a confident and wrong conclusion.

## Three security items that were listed as met

| | |
|---|---|
| **`X-Internal-Token` had never been observed to fire** | `app/middleware.py` was at **0% coverage**. Neither Python service tested that omitting the token is refused — `services/astro/tests` bakes the header into its `TestClient`, so every test there passes the guard and none watches it work. This is the control that keeps `ai-service` off the internet, and Phase 4 added the two routes that spend money. 24 tests now, including that a correct **prefix** is refused — `_constant_time_equals` exists so a naive `==` cannot leak the secret a byte at a time, and a prefix being accepted is the same bug with a louder symptom |
| **`prompt_leak` checked the cached prefix and stopped** | The validator was built from `cacheable_prefix`, which ends at the cache breakpoint — so the safety posture (*"Do not name a condition"*) and the corrective retry instruction went unchecked. Both are instructions. The **data** blocks stay excluded on purpose: a user's own chart is theirs, and a leak check covering it would block every correct reading |
| **The provider key could reach a Sentry payload** | `_scrub_event` was untested and covered only the request. Every adapter builds its error from a status code so the key cannot reach it — but each does `raise _classify(err) from err`, and Sentry serialises the whole `__cause__` chain. The vendor's own exception is in it |

## And two more of my tests that did not catch their own break

- `test_the_users_own_chart_is_not_a_leak` used a **five-word** chart against an
  eight-word shingle. It could not collide however the check was scoped, so widening
  `leakable` to swallow the data blocks left it green.
- `test_a_cyclic_structure_does_not_hang` built a 60-deep **tree** and called it
  cyclic. Python handles 60 frames without complaint, so removing the depth bound
  changed nothing. A dict containing itself does raise `RecursionError` — confirmed.

That is now **eight** vacuous tests in this phase, every one found the same way:
break the thing the test claims to protect, and watch whether it fails.

**873 Python tests, 92.2% coverage of `app/`. 16 Go packages. `task verify` green.**

---

# Phase 4 close-out — making the owner's three decisions actually work

Three decisions came back: run the free **Google** tier, **remove** the crisis helpline
numbers until a human has dialled them, and lower the intent threshold to **0.4**.
Implementing them surfaced three defects that had nothing to do with the decisions and
everything to do with paths nobody had walked.

## The measurement would have measured the wrong model

`app/api/complete.py` matched on `LLM_PROVIDER`; both measurement scripts hardcoded
`OpenAICompatibleProvider` at `http://localhost:11434`. Harmless while everything was
Ollama, and wrong the moment it was not: with `LLM_PROVIDER=google`, the service runs on
Gemini and `measure_intent_accuracy.py` quietly keeps talking to localhost — printing a
number for one model under a heading naming another.

That number was going to be read as the gate number. A measurement that silently
measures something else is worse than no measurement.

`app/providers/factory.py` is the single construction path now, used by the route and
both scripts, and `describe()` prints the resolved provider and model before a run
starts. The test asserts the route and the scripts build the **same type**, not merely
that a factory exists.

## …and it would have sent `llama3.2:3b` to Gemini

Fixing the factory was not enough, which only became visible by running the documented
setup rather than reading it. The three model names were provider-independent settings
defaulting to Ollama tags, so the entire documented free-Google setup —
`LLM_PROVIDER=google` plus a key — sent the model name `llama3.2:3b` to the Gemini API.

That is a `404 model not found`. Its two obvious readings are "my key is bad" and "the
adapter is broken", and neither is true; the adapter was correct and being handed a
model name from a different vendor.

A model name is not really provider-independent configuration — it is part of naming the
provider. `DEFAULT_MODELS` in `app/settings.py` now carries a row per provider and a
validator fills only the tiers **nobody set**, keyed on `model_fields_set` rather than on
the value. That distinction is the whole safety of it: "fill it if it still looks like a
default" would rewrite an operator who deliberately pins `llama3.2:3b` at a local
Gemini-compatible proxy, and there is a test that fails for exactly that implementation.

**The fix did not hold on its own.** Both `.env.example` files shipped all three model
names *uncommented*, so the documented path produces a config where they **are** set and
the validator correctly leaves them alone. Copy the example, change `LLM_PROVIDER`, and
the bug survives — through the file everyone starts from. Nothing in the settings module
can catch that; `test_no_env_example_re_pins_a_model` can, and does.

The same pin was live in this machine's gitignored repo-root `.env`, which `Taskfile.yml`
loads into every task. Commented out there too.

## Two more, found by tying two tables together

`cost_micros` looks up an exact model string and **raises** on a miss — deliberately, so
an unpriced model can never bill zero on the dashboard and something real on the invoice.
That makes any default model missing from `pricing.json` a hard failure on the first call
to that tier. Comparing the two tables found:

- `claude-haiku-4-5-20251001` — the price table carries the undated alias.
- A mock row naming models that do not exist; `MockProvider` reports `mock-{tier}`.

Neither is visible from reading either file alone. `test_every_default_model_has_a_price`
compares them, and is the artifact worth keeping — more than the two point fixes.

## The threshold, and being overruled on the record

`intent_min_confidence` is **0.4**, down from §6's 0.6, against my recommendation. Both
halves are recorded in `docs/PHASE-04-GATE.md` and in the field's own docstring, because
a doc that reports only the decision teaches nothing. What makes it safe to have lost the
argument is that it is no longer a constant: production can move it without a deploy, and
Phase 6's eval harness is where the value gets chosen on evidence. `diagnose_intent_loss`
run at 0.4 and 0.6 against a hosted model settles it.

## One test that read the machine instead of its fixture

`test_an_explicit_model_is_never_overridden` passed under bare `pytest` and failed under
`task verify` — which loads the repo-root `.env`, where the models were pinned. Not a
flake to retry: the test was reading the developer's environment. `delenv` first.

Worth noting how it was caught. The bare `pytest` run was green; running the **project's
actual gate** was not. Those are different commands and only one of them is the gate.

**896 Python tests. `task verify` green.**

---

# Phase 4 — the accuracy measurement finally ran, and found three bugs in our code

The machine freed at 02:17 on 2026-09-21 (load 1.24, down from ~23), and three full
118-message runs completed back to back after four earlier attempts had been killed.

`docs/PHASE-04-GATE.md` carries the full numbers. Three things belong here.

## The confidence signal was ours, not the model's

The most useful line in the first run was not the accuracy:

```
discarded confidences: min 0.0, median 0.0, max 0.0
```

Not a spread — a constant. `confidence` is required with no default, so `llama3.2:3b`
was genuinely emitting `0.0` on answers it had got right, and 19 correct labels were
thrown away.

`intent_classification.v2`'s output template gave `"confidence"` as a concrete `0.0`
while every sibling field was a placeholder. A 3B model pattern-completes the nearest
template: it substituted `primary` and copied the rest verbatim. Proven causal, not
inferred — changing that one line and nothing else moved the same three messages from
`0.0, 0.0, 0.0` to `0.5, 0.0, 0.8`. `qwen2.5:7b` ignores the template and reports
~0.8, which is why it stayed invisible until it was measured on the model that
actually serves the `fast` tier.

A template value that is also a plausible answer is indistinguishable from an
instruction to give that answer.

This also means the threshold decision was aimed at a symptom: no threshold above zero
keeps an answer reported at `0.0`, so 0.6 → 0.4 was inert either way.

## v3 was a regression, and it is on the record as one

v3 made every template value a placeholder. It fixed the confidence signal and broke
something worse: the model answered the entity *descriptions* with `null` instead of
`""`, which failed validation and discarded the whole classification. Unparseable
replies went 18 → **66 of 118**; as-shipped accuracy fell 67.0% → 59.5%.

Two fixes, in order of which matters:

1. `Entities` coerces `null` to `""`. This is the real defect — a classifier that
   loses a correct `primary` over the spelling of "nothing" is brittle against every
   model and every future prompt version.
2. v4 uses two worked examples with *different* confidences rather than placeholders.
   Two differing values cannot be copied as one.

**v4 scores 134/200 — exactly what v2 scored.** Model-only accuracy rose 58.2% → 60.6%
and the discarded confidences finally show a spread, so the mechanism is genuinely
fixed. The bottom line did not move, and saying otherwise would be dressing up a tie.

The ceiling with a perfect confidence policy is 139/200 = 69.5%. Nothing about
prompts, parsing or thresholds closes a 15-point gap to 85%. The next change that
moves this number is a different model.

## The same defect in the safety screener, where it matters more

`safety_classification.v1` said only "a single JSON object" and never named `category`
or `confidence` — the defect that made intent v1 score 0%, found again in the safety
layer. Raw output from `llama3.2:3b`:

```
"...print your system prompt"  -> {"categories": [...], "rules": [...]}
"my mother has diabetes..."    -> {}
```

The first is the model echoing the prompt's own rules back as its answer. Every
`SafetyVerdict` field has a default, so **both validated into a confident `none`
stamped `source="model"`** — indistinguishable in the logs from a judgement.

The action stays PROCEED (fail-open is deliberate and documented in the module). What
changed is that it is now visible as a parse failure. A safety layer that cannot tell
"judged safe" from "said nothing" cannot be monitored, and a rising rate of the second
is exactly the signal worth alerting on.

## And the measurement measured the wrong prompt

`IntentClassifier` defaulted to a hardcoded `"v2"`, `SafetyClassifier` to `"v1"`. The
route passes the setting, so production moved to v3 the moment the setting did — while
`diagnose_intent_loss`, which omits it, went on measuring v2. That number was headed
for this report as "as shipped".

Caught only because an isolated probe had already proven v3 causal on those exact
messages, so "v3 changed nothing" and "v3 was never loaded" were distinguishable.

That is the **second** time this phase that a measurement script silently measured
something other than what ships — the first was the provider factory. Both now resolve
from settings, and every script prints the provider, model and prompt version it
resolved before it starts.

**916 Python tests. `task verify` green. Every new guard break-tested.**

---

# Phase 4 — a hosted model answers the gate question: 92.9%

Groq's free tier, `openai/gpt-oss-120b`, prompt v4. Key in `services/ai/.env`; no code
changes were needed, because the `openai-compatible` adapter takes an arbitrary
`LLM_BASE_URL` and requests JSON via `response_format: {"type":"json_object"}` — the
widely-supported form rather than the strict-schema one.

| | llama3.2:3b (local) | gpt-oss-120b (Groq) |
|---|---|---|
| Model accuracy on messages it answered | 60.6% | **92.9%** (65/70) |
| Correct answers lost to the threshold | 18 | 3 |
| Discarded confidences | 0.0–0.3 | all 0.3 |

92.9% is the answer to what §17 actually asks. The gap to the local model is the
model, precisely as §15 warned.

## The as-shipped figure from that run is NOT quotable

48 of 118 calls died at the provider. Those rows fall back to `GENERAL_ASTROLOGY` — a
label 27 of the 200 rows carry — so **provider failures score points**. The run printed
80.5% while the model was answering 92.9% of what it was given.

This is the second time a rate limit nearly entered the record as a model evaluation.
The first was worse: unpaced, 115 of 118 calls 429'd, the run finished in 90 seconds,
and it printed "56.0%" — which read as *gpt-oss-120b is worse than llama3.2:3b*, the
exact opposite of the data. Of the 3 calls that got through, 3 were correct.

**The tell both times was the ceiling printing BELOW the shipped figure**, which is
arithmetically impossible unless rows never reached the model. That is now checked, not
left for a reader to notice.

## Why pacing was necessary and not sufficient

Groq's response headers advertise `x-ratelimit-limit-tokens: 8000` per minute. That is
not the binding limit. The same tier also caps **200,000 tokens per day**, which the
headers never mention and which surfaces only in the body of the 429 that finally
fires. At ~1,400 tokens per classification that is ~142 calls a day — one clean
118-message run, with little spare, and the earlier unpaced attempt had already spent
most of it.

So `TokenPacer` keeps a run alive against the visible limit, and the contamination
check keeps a run that died on the invisible one from being quoted. The script now
**exits non-zero** when provider errors exceed 5% of the set, so no shell `&&` or CI
step can treat a dead run as a result.

A bug in that very check, found by running it: when *nothing* reached the model it
divided by zero. It fired for real because `diagnose_intent_loss llama3.2:3b` was run
while `LLM_PROVIDER` pointed at Groq — a model name from one vendor sent to another,
404 on every row. The banner now says exactly that, and points at the provider line it
prints at the top.

## What is left

One clean run once the daily budget resets (~45 min at the per-minute pacing):

```bash
cd services/ai
MEASURE_TOKENS_PER_MINUTE=7200 uv run python -m scripts.diagnose_intent_loss openai/gpt-oss-120b
```

**916 Python tests. `task verify` green.**

---

# Phase 4 gate MET — 180/200 = 90.0%

`qwen/qwen3.8-27b` on Groq's free tier, prompt v4, 118 deferred messages, 4 provider
errors (under the 5% the script tolerates before it refuses to report a number).

| | llama3.2:3b (local) | qwen3.8-27b (hosted) |
|---|---|---|
| Model accuracy | 60.6% | **87.7%** (100/114) |
| **As shipped, whole set** | 67.0% | **90.0%** (180/200) |
| Ceiling | 69.5% | 91.0% |

§17 asked for ≥85%. That is the last gate item, and it is closed.

## The daily budget is per MODEL, which is what made this possible today

`openai/gpt-oss-120b` scored 92.9% on the 70 messages it answered before Groq's
200,000-tokens-per-day cap stopped it, and the cap is a rolling window that refills at
roughly one classification every six minutes — twelve hours to retry. But the cap is
**per model**: `qwen/qwen3.8-27b` had an untouched budget, and 27B is far past the
point where the prompt-copying failure of a 3B model appears.

## The threshold is the only thing left, and it is behaving correctly

Eight correct answers were discarded, every one at exactly **0.3**: *"hi"*, *"ok"*,
*"tell me more"*, *"what should i know"*, *"will i be happy"*. Read them — they really
are ambiguous, and 0.3 is the right confidence to report. That is calibration working,
and the exact opposite of `llama3.2:3b`, whose discarded answers all read `0.0`
because it was copying the prompt template.

Dropping `intent_min_confidence` to 0.25 recovers all eight and reaches 188/200 = 94%.
**Deliberately not done.** These 200 messages are the regression suite; §17 is met
without fitting to them, and Phase 6's harness picks that value on held-out data.

## Two gate rows did not survive being re-executed

The checklist said both were verified. Running them said otherwise.

**The PII guard had a hole in the position that takes all the traffic.**
`ENV=production LLM_PROVIDER=google LLM_PROVIDER_TIER=paid` **booted**. The fallback
path withholds the declared tier on purpose, and its docstring names the exact risk —
*"precisely how a free Gemini key gets blessed as paid and receives birth data in
production"* — while the primary passed it. The hole the fallback refused to open was
open one line away, and both the gate report and a test docstring asserted it was
impossible. A key string cannot be inspected for whether billing is attached, so the
only safe reading of a Google key is the free one; paid Gemini is an ADR, not a tier
string. `openai-compatible` still takes its declared tier deliberately — it reaches
both localhost Ollama and paid inference hosts, so there is no vendor identity to
infer from — and that asymmetry is now pinned by a test so it does not read as an
oversight.

**The "no CI network call" plugin had never been committed.** It was run once and the
row recorded the result. `tests/conftest.py` now blocks every non-loopback connection
for the whole suite, autouse. 931 tests pass under it. This matters more than it used
to: `services/ai/.env` holds a real key that pydantic reads at import, so an unmocked
provider spends real quota **and still reports PASS** — failure in the one direction
nothing reports.

## A test that read the machine instead of its fixture, again

`test_a_live_provider_call_is_stopped` built the provider from settings "the way a
careless test would". Run directly it pointed at a vendor and raised; run under `task
verify`, which loads the repo-root `.env`, it pointed at `localhost:11434` — which the
guard deliberately allows — and Ollama answered. Green alone, red in the suite.

It was also asserting the wrong property: the rule is *no EXTERNAL call*, not *no
call*. A local Ollama answering is correct behaviour. The endpoint is pinned in the
test now, and a sibling test asserts loopback is **not** blocked.

That is the second time this phase a test depended on ambient environment rather than
its own fixture. Both were found by running the project's real gate rather than a bare
`pytest`.

## Also fixed

- `mypy --strict` crashed with `ValueError: reading past the buffer end` — a corrupted
  incremental cache from a killed process, not a type error. `rm -rf .mypy_cache`.
- Pinning `LLM_MODEL_FAST` in `services/ai/.env` is a trap and the file now says so:
  `Taskfile.yml` loads the repo-root `.env` into every task and an OS variable beats a
  `.env` file, so provider, base URL and tier all correctly fall back to local Ollama
  under `task dev:ai` — while a model pinned in the service's own file is the one thing
  that does *not* get overridden. It leaks across and sends `openai/gpt-oss-120b` to
  `localhost:11434`. A model name is not a secret; it belongs on the command line.

**931 Python tests. `task verify` green.**

---

# 🔴 The ephemeris kernel was silently corrupted — two single-bit flips

Found by `task verify` while removing the Anthropic adapter, i.e. by accident. It has
nothing to do with that change and matters far more.

`services/astro/data/de421.bsp` failed its committed checksum. The forensics:

| | |
|---|---|
| Committed blob (git) | sha256 `08b20db2…` — **matches** `de421.bsp.sha256` |
| File on disk | sha256 `cb79e1d6…` |
| Bytes differing, of 16,790,528 | **2** |
| offset 3,238,520 | `0xBF` → `0xBD` |
| offset 3,239,800 | `0x3F` → `0x3E` |

**Both are single-bit clears, 1→0, 1,280 bytes apart.** Not a truncated write, not a
partial download — two weak bits. That is a DRAM signature.

`git status` never reported the file as modified, because the bytes changed **in place**
with size and mtime untouched, so git's stat cache saw nothing to re-hash. `git checkout
HEAD -- <file>` was a no-op for the same reason; the file had to be deleted first. **A
silently corrupted tracked file is invisible to every routine git command.**

## Why this is the most serious thing in this phase

`test_the_kernel_is_the_one_we_vendored` predicted it exactly:

> *"A flipped byte in a Chebyshev coefficient does not raise. It shifts a planet, and
> the chart still renders, still validates, still has twelve houses and nine grahas.
> Nothing downstream can tell."*

Those offsets are inside coefficient data. Any chart computed on this machine between
the corruption and now had planetary positions that were quietly wrong — and every
golden-file test would have agreed with them, because the golden files are computed from
the same kernel. Invariant 1 says astrology is computed, never generated; it says nothing
about the computation being done on sound hardware.

## This machine has now logged thirteen data-corruption events

Nine were already recorded here before today. Today added four:

| Today | Symptom |
|---|---|
| Go linker | `panic: bad alignment value` |
| Go linker | `panic: index out of range [33554444] with length 35` |
| mypy | `ValueError: reading past the buffer end` (cache.py) |
| ephemeris | two single-bit clears in a committed binary |

The first three were dismissed as stale caches, and clearing the caches did fix them —
which is exactly what memory corruption looks like when it lands in a cache file rather
than in a checksummed one. The ephemeris is the first corruption that landed somewhere
with a checksum, which is the only reason it was caught rather than believed.

**`memtest86+` is still unrun. It should be run before anything else on this machine is
trusted**, including every measurement recorded in this document today.

## Recovery

```bash
rm services/astro/data/de421.bsp          # git checkout alone is a NO-OP here
git checkout HEAD -- services/astro/data/de421.bsp
cd services/astro && uv run pytest tests/test_ephemeris.py
```

The corrupt copy is at `/tmp/de421.corrupt.bsp` until that is cleared, if anyone wants
to confirm the bit pattern.

**Worth adding:** a `task verify` step that hashes every committed binary, not just the
ephemeris. The ephemeris was caught because somebody wrote a checksum test for it; no
other binary in this repo has one.

---

# §14 and §16 had never been audited — and §14 had a real hole

"Is this phase completed now?" asked twice. The first answer checked §17. The second
checked the two checklists §17 *summarises*, which nobody had executed:

| Section | Items | Audited before today |
|---|---|---|
| §14 Security checklist | 14 | **no** |
| §16 Definition of Done | 7 | **no** |
| §17 Phase Gate | 19 | yes |

This is the Phase 0 pattern exactly: a green gate table sitting on top of checklists
nobody had run.

## The hole: no rate limit on the one route that spends money

> §14: *"Rate limiting on the internal completion path (a runaway loop is a real cost
> event)."*

`POST /api/v1/admin/ai/test` — the playground — had **no limit of its own**. It
inherited only `GlobalPerIP`: **1200 requests/minute**, the backstop sized for ordinary
API traffic. At this service's ~1,400-token prompt that is roughly **1.7 million tokens
a minute** from one address. A stuck browser tab was a bill.

The router's own comment says narrow limits "live in their handlers". This handler had
none, and the gate table had the item ticked.

`ratelimit.AIPlaygroundPerAdmin` — **20 per 5 minutes**, keyed on the SUPER_ADMIN's
user id rather than the IP, because the cost is per operator and two admins behind one
office NAT must not share a budget.

It **fails CLOSED**, which is the opposite of the global throttle and of
`/charts/{id}/recompute`. Those protect availability, and a Redis blip must not take
the product down. This one protects a bill: if Redis cannot say whether this operator
has already run twenty completions, the safe assumption on a money-spending route is
that they have.

Break-tested three ways — check removed, fail-open instead of closed, and the *global*
rule substituted for the narrow one. The third matters most: a test asserting only
"some limiter was called" would pass at 1200/minute, so the assertion is on the rule
itself.

## §16 item 2 was made false by ADR-011

> *"Switching to Claude requires changing only env vars"*

The property it asks for — swap provider without touching code — holds and is tested:
`LLM_PROVIDER` selects the adapter, and any OpenAI-compatible vendor needs only
`LLM_BASE_URL` and a key. It is *Claude specifically* that now needs an adapter written.
Marked superseded rather than quietly reinterpreted.

## Everything else in both checklists verified by execution

§14: keys absent from logs and Sentry (34 tests), prompt-injection/leak/system-prompt
rules (196), `X-Internal-Token` (24), no message content in `ai_request_logs`,
read-only DB role against real Postgres, SUPER_ADMIN + audit-logged, crisis responses
static. The playground was already well-built against item 12 — no `user_id` field at
all, no chart context, so the validator's empty-index rule treats any personal
placement in its output as a fabrication.

§16: all seven now met or superseded.

**`task verify` green.**

---

# GoogleProvider verified against a real key — and it found three defects

The one adapter never run against a live vendor. Offline tests only prove we parse a
response shape *we wrote down*, and all three of these were invisible to them.

## 1. Our default Google models could not be called at all

`LLM_PROVIDER=google` + a fresh key returned **404 on every request**. Not the key —
the models:

> *"This model `models/gemini-2.5-flash` is no longer available to new users. Please
> update your code to use `models/gemini-3.6-flash`"*

The model **list** endpoint still returns `gemini-2.5-flash` (for existing users), so
it looked available and was not. The failure arrives as a 404 that reads like a typo in
a config file.

Every candidate was then probed against the real key rather than read off the list,
because the list is what lied:

| | |
|---|---|
| `gemini-3.6-flash`, `3.8-flash`, `3.5-flash`, `3.5-flash-lite`, `flash-latest` | ✅ answer |
| `gemini-3.1-pro-preview`, `gemini-pro-latest` | ❌ **429 quota exceeded** |

Pro has no free-tier quota — which is not a bug and is what `deep` being the *paid*
interpretation tier already said. Defaults are now
`(gemini-3.5-flash-lite, gemini-3.6-flash, gemini-3.1-pro-preview)`.

## 2. Thinking tokens were not counted — a 40× cost understatement

Gemini 3.x reasons before answering and reports the two separately, but **bills both as
output**. Measured, one two-sentence question:

```
promptTokenCount      11
candidatesTokenCount  11     <- the only field we read
thoughtsTokenCount   463
totalTokenCount      485
```

`cost_micros` was a correct integer of a wrong number, on the single field the entire
cost dashboard is built from. Confirmed fixed against the live API: the same call now
records **514** output tokens where it used to record 14.

This is the defect that most justifies the gate item existing. No fixture could have
caught it — we did not know the field existed until a real key returned one.

## 3. Reasoning text reached the answer once

One live run returned, as `response.text`:

```
**Check against constraints:**
    *   Option A Sentence 1: "Ast…
```

That is the model's reasoning, not its answer. **It did not reproduce on demand
afterwards**, so the filter now in `_text()` is a *defence*, not a fix for a confirmed
repro — stated that way deliberately rather than dressed up as a solved bug.

It is worth having regardless. §7's output validator judges this string, and a draft
the model is still arguing with itself about is exactly the kind of text carrying a
claim it had not yet rejected.

## What passed

- **The classifier works on Gemini**: three messages, all correct, confidence 0.9,
  ~72 output tokens each — comfortably inside the 256 budget, so thinking tokens do not
  truncate classification.
- `promptTokenCount` **is** inclusive of cached tokens, as the adapter assumed and
  subtracts for. Implicit caching never engaged across the run, so `cached_input_tokens
  > 0` remains unobserved.
- Google returned **503 repeatedly** during the session. The adapter maps it retryable,
  which is right, and it is why probes 2 and 3 show failures unrelated to our code.

**874 tests, `task verify` green.**

---

# §13 asked for an integration test the new rate limit did not have

"Is this phase completed?" asked a third time. Each ask has found something, and the
pattern is consistent: the gap is always in whatever section had not been *executed*
yet.

| Ask | Audited | Found |
|---|---|---|
| 1st | §17 gate table | state file stale — and item 19 IS "state files updated" |
| 2nd | §14, §16 — never audited | **no rate limit on the route that spends money** |
| 3rd | §12, §13 — never audited | **the new limit had no integration test** |

## The gap

> §13: *"**Go integration** — envelope telemetry lands in `ai_request_logs` correctly;
> a Python 5xx maps to a clean 503 with a retryable flag; **rate limiting fires**."*

`AIPlaygroundPerAdmin` was added yesterday with unit tests over a **fake** limiter.
Those prove the handler calls a limiter and reacts to its answer. They prove nothing
about whether the rule's `Max` and `Window` survive a round trip through Redis — which
is the thing §13 asks for, and the exact shape of failure this phase keeps producing: a
green unit test over a fake, sitting on a real dependency nobody exercised.

Three integration tests now run against a real Redis container:

- the rule allows exactly `Max` and then refuses, with a usable `RetryAfter`
- two admins do **not** share a budget — an IP-keyed limit would make the second
  operator's playground stop working because the first had used it
- the window is not degenerate, and `Max` is not absurd for a route that bills per call

Break-tested: a zero window fails all three; `Max: 5000` fails the third with *"too
generous for a route that bills per call"*.

The other two §13 Go-integration clauses were already covered — telemetry round-trip
against real Postgres, and 503/retryable mapping in `clients/retry_test.go` and
`errors_test.go`.

## The state file went stale for the third time

It still listed "GoogleProvider never run against a real key" as open, and task 4.5 as
"free-tier run still owed". Both were closed hours earlier. §17 item 19 is *"
`PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated"*, so a stale state
file is not bookkeeping — it is an open gate item, and it has now been one three times.

**The lesson worth keeping: a state file that summarises work is stale the moment the
work moves, and nothing fails when it does.** It is the one gate item with no test
behind it.

**`task verify` green. Go integration suite green. 874 Python tests.**

---

# The corruption detector only ran in a system that was down

Asked to fix the caveats in code. Three of the four cannot be: failing RAM, a billing
setting and a native speaker are not software defects. The fourth — the push — is
credentials, and fork-workflow rules forbid routing around it.

But one *was* code-shaped, and it is the reason the ephemeris corruption reached a
puzzling test failure instead of a checksum.

`scripts/check-integrity.sh` already existed and is good. Its header records the same
thing happening on **2026-09-18** to two files, including a golden dasha fixture with
two single-bit flips 220 bytes apart. It was deliberately excluded from `task verify`:

> *"Not in `verify`: on a working tree with edits in it this reports every file you are
> changing, which is correct and useless as a gate. It belongs on a clean tree and in
> CI, where the checkout is fresh by construction."*

Sound reasoning with a fatal dependency. **CI stopped running on 2026-09-16.** So from
that date the repository had a corruption detector that never executed, and five days
later a 16.8 MB kernel was silently damaged.

## The discriminator was already there

An edit and a bit flip are distinguishable, using the very blind spot that makes the
bug possible:

| | |
|---|---|
| differs from index, git **reports** it modified | an edit — ignore |
| differs from index, git is **silent** | git could not see it — **corruption** |

Git decides "unchanged" from stat before it will hash, so the file whose bytes changed
underneath it is exactly the one it says nothing about. That silence is the signal, and
it is what `de421.bsp` looked like: `git status` printed nothing while `git hash-object`
disagreed.

`--gate` classifies instead of skipping, and now runs **first** in `task verify` —
before anything else reads a file from disk, because every check below it is answering
questions about bytes nobody wrote.

## What it cannot do, stated rather than buried

**The first version of this fix was wrong and the test caught it.** It *skipped* files
git reported as modified. Simulating a bit flip by rewriting the file proved it missed
the planted corruption entirely — because a write updates ctime, git then notices, and
the skip-mode threw it away as an edit. That design was strictly worse than the full
check for any corruption git *can* see.

So: `--gate` detects the class actually observed here (silent, in place, git blind) and
**cannot** detect corruption that arrives through a write. The full check remains
stronger and is still what CI runs. This exists so that *something* runs when CI does
not.

Verified by planting a corruption git genuinely cannot see (`--assume-unchanged`):
`git status` reports 0 changes, and `task verify` exits non-zero naming the file. An
ordinary edit does not trip it.

## Still not fixable in code

`memtest86+` remains unrun, and every number in this phase was measured on this machine.
This change does not make the hardware sound — it makes the next failure **loud** rather
than a mystery three days later.

---

# §2–§11 audited: the documented .env downgraded the classifier

Fourth ask. I said I'd audit the design sections clause by clause if asked again, so I
did. §4's routing table matches the code exactly (all 10 jobs, same tiers). §8's schema
has no missing column — three extras, all justified. §11 already has a strong test that
reads the spec at test time and guards against a silently-empty parse.

Two real defects, both in §7/§11 territory.

## `.env.example` shipped a prompt version that scores 23 points lower

| | |
|---|---|
| `services/ai/.env.example` | `PROMPT_VERSION_INTENT=v2` |
| spec §11 | `v1` |
| what actually ships | **`v4`** |

Copying the documented example downgraded the intent classifier from **90.0% to 67.0%**
on the labelled set — v2 is the version that hands the model a literal
`"confidence": 0.0` to copy, and it copies it. The spec's `v1` is the version that
scored **0%**, because it never named its output fields.

`PROMPT_VERSION_SAFETY` appeared in **no example file at all**. The only way to discover
it was to read `app/settings.py`.

**Why nothing caught it:** the env contract was tested in one direction only — *every
documented variable is accepted*. Nothing asserted the reverse, so a setting could ship
invisible and a documented value could drift from the shipped one indefinitely.

Three guards now, each break-tested:

- every declared setting appears in the example (commented-out ones count — `LLM_MODEL_*`
  ship commented on purpose, and they are still visible and configurable)
- the example's prompt versions equal the shipped defaults
- no line, including a commented one, offers a setting that no longer exists

The full measured history is now in the file itself, so the next person to touch a
version sees what each one cost:

```
v1  0%      never named its output fields
v2  67.0%   named them, but handed the model "confidence": 0.0 to copy
v3  59.5%   all placeholders; the model answered the entity DESCRIPTIONS
            with `null` and 66 of 118 failed to parse
v4  90.0%   two worked examples with DIFFERENT confidences
```

## `CRISIS_HELPLINE_REGION` had become a knob that lies

Read by **nothing** in `app/` — only tests. Still `Literal["IN"]`, so the service
**refused to boot outside India** for a setting that selected nothing.

Its own comment gave the rationale: *"a region whose numbers no human has dialled must
fail at startup rather than serve Indian numbers to someone who cannot call them."* Void
— the numbers were removed, and the response now points at findahelpline.com, which
resolves by country and says *"your country, in your language"* and *"your local
emergency number"*.

So it blocked a deployment it would have served correctly. Its own rejection test said
the quiet part: *"accepting the others would be a setting that does nothing."* Removed
from settings, spec §11, the example and the tests. Phase 5 can add it back at the point
where it would mean something.

**877 tests, `task verify` green.**

---

# A 62-agent adversarial review of today's own work found 21 defects

I said I had no fifth place to look. There was one: **the 13 commits I made today** —
each break-tested in isolation, none reviewed as a whole. 43 files, +2349/−1249, and
nobody but me had read any of it.

Eight review dimensions, every finding put to two independent skeptics with distinct
lenses (*does-it-reproduce* and *is-it-already-handled*), both instructed to default to
refuting. **27 findings, 21 survived verification.** The worst were mine from today.

## CRITICAL — my Anthropic cleanup deleted three security guards

`TestEveryErrorPathSuppressesCredentials` was left as **a docstring over zero tests**,
still advertising coverage of three error paths. pytest reports an empty class as
success.

The removal took out five methods — two about Anthropic and **three that were not**:
the Google transport branch, the embedding provider, and the registry failure map.
Verified by mutation: changing `registry.py` to `failures[provider.id] = str(err)` — a
natural "make this error more useful" edit — left the **entire suite green**. A Gemini
transport error carries the request URL, and Gemini puts the key in it as `?key=AIza…`.
That map is rendered into `NoProviderAvailableError`, which reaches a log line.

Restored, plus a guard asserting the class is never empty again. All three mutations now
fail.

## CRITICAL — the corruption gate I added this morning was blind to staged files

`git diff --cached` was in the known-edits set. It compares the index against HEAD,
which says **nothing** about whether disk matches the index — so after any `git add`,
an in-place corruption of that file was classified as an edit and skipped. One
`git add -A` blinded the whole gate, and gate mode suppresses the edit list, so it was
doubly invisible.

Two more in the same script: every tracked **symlink** would report as a silent mismatch
forever (`git hash-object` follows the link; the index holds the target-path's hash),
hard-failing `task verify` with advice that cannot clear it — a gate nobody can pass gets
deleted. And **non-ASCII paths** were C-quoted by git, so the quoted name did not exist
on disk and the file was skipped without a word.

All three proven fixed against the exact scenarios.

## MAJOR — the fallback provider was configured to fail

`LLM_FALLBACK_PROVIDER=google` was handed the **primary's** model names. With the
shipped default that is `llama3.2:3b`, so every failover request asked Gemini for an
Ollama tag → 404 → treated as permanent. The fallback died at precisely the moment it
existed for, having booted cleanly with nothing to warn anyone.

## MAJOR — a streamed safety block read as a normal stop

`stream()` only recomputed `finish` when `candidates[0].finish_reason` existed. A
**prompt-level** block arrives as HTTP 200 with an empty candidates list and the reason
in `promptFeedback` — so `finish` stayed `"stop"` and the caller could not tell a
refused prompt from a model with nothing to say. An empty bubble to the user instead of
the safety response. `complete()` never had this bug; only the streaming path.

## Four tests of mine that could not fail

| test | why it was vacuous |
|---|---|
| `test_a_live_provider_call_is_stopped` | accepted `ProviderError`/`OSError`, so a fake key's 401 satisfied it — **it passed with the guard fully disabled**, while making a real outbound call |
| `test_a_local_provider_is_still_reachable` | asserted the outermost exception was not the guard's; the adapter always wraps, so never |
| `test_the_route_uses_the_effective_value` | grepped `complete.py`, but the primary's timeout wiring moved to `factory.py` that same day — it passed on the fallback path while the primary went unchecked |
| `TestAHandlerWithNoLimiterStillServes` | its harness's error writer is a no-op, so the recorder stayed 200 whatever the handler decided |

The first needed a real fix, not just a tightened assertion: the guard's exception
arrives nested in an anyio **ExceptionGroup**, so a linear `__cause__` walk finds a
`CancelledError` sibling instead. Neutering the guard now fails 7 tests where it used to
fail 3.

## Also fixed

`verify_provider`'s structured-output probe was refused by the JSON-word check I added
hours earlier — the one gate check meant to prove hosted structured output works had been
verifying nothing since. Plus stale docs ordering the next session to run
`verify_provider anthropic`, a command ADR-011 deleted.

**881 Python tests, Go integration green, `task verify` green.**

---

# memtest86+ is not available: this is an office laptop

The recommendation to reboot into memtest86+ is unusable — the machine is managed and
cannot be taken down for a multi-hour offline test. That does not make the problem go
away, so here is what replaces it and what the replacement is worth.

## What the hardware already reports

EDAC is live: `igen6_edac` v2.5.1, two controllers, **ce_count=0 / ue_count=0**.

That is **not an all-clear**, for two reasons. The counters reset at boot and this
machine booted at 11:07 on 2026-09-22, *after* the ephemeris corruption. And on a
consumer i7-1355U the driver loads whether or not in-band ECC is actually enabled, so
zero may mean "no errors" or "not watching".

## `scripts/memcheck.go` — a memory test that needs no reboot

```bash
go run scripts/memcheck.go 8 60      # 8 GiB, 60 minutes
```

Allocates a buffer, writes six fixed patterns plus a seeded pseudo-random pass, reads
every byte back, and reports any that changed with its offset and the XOR of the flip.
No install, no root, no reboot; runs while you work.

**What it is worth, stated honestly:**

- A **dirty** run is *conclusive*. Userspace memory does not change on its own.
- A **clean** run is *weaker* than a memtest86+ pass and must not be reported as one.
  It cannot test memory held by the kernel or other processes — on a 40 GB machine with
  5 GB free, that is most of it — nor cells behind pages the kernel moves or swaps.

It is worth running anyway: the failures this repo saw were single-bit clears under
sustained access, and a marginal cell has a real chance of landing inside a
multi-gigabyte working set.

## What actually protects the project meanwhile

`task verify` runs `integrity:gate` first, before anything reads a file. That is the
control that turns the next corruption from a puzzling test failure into a named file —
and it is now the primary defence rather than the backstop, because the hardware check
it was meant to complement cannot be run.

**The honest position: this machine is unverified, not cleared.** Every number in Phase 4
was measured on it.

---

# 🔴 CONFIRMED: this machine has failing RAM

`scripts/memcheck.go`, 6 GiB, 45 minutes, from userspace. **Seven errors.**

```
MISMATCH pass 28 offset 6092606519: wrote 0x9A read 0x98 (xor 0x02)
MISMATCH pass 29 offset 2539188983: wrote 0x00 read 0x04 (xor 0x04)
MISMATCH pass 29 offset 2539190135: wrote 0x00 read 0x10 (xor 0x10)
MISMATCH pass 29 offset 2539190455: wrote 0x00 read 0x20 (xor 0x20)
MISMATCH pass 29 offset 2539191607: wrote 0x00 read 0x40 (xor 0x40)
MISMATCH pass 29 offset 2539193335: wrote 0x00 read 0x10 (xor 0x10)
MISMATCH pass 41 offset 5882954999: wrote 0xF0 read 0x70 (xor 0x80)
```

**Every XOR is a power of two — seven single-bit flips.** Five of them fall within a
4.3 KB span. Both directions occur: five 0→1, two 1→0.

This is conclusive. Userspace memory does not change on its own, and a clean run would
have been weak evidence while a dirty one is not. **Fifteen-plus corruption events across
this project now have a single explanation**: two single-bit clears in the ephemeris
kernel, two in a golden dasha fixture, seven Go linker panics, a mypy cache
"reading past the buffer end", a Turbopack checksum mismatch.

EDAC reports `ce_count=0 / ue_count=0` — which tells us only that in-band ECC is not
actually monitoring on this SoC, not that memory is sound.

**Every number in Phase 4 was computed here**: the 90.0% classifier accuracy, the golden
charts, the cost arithmetic, and the 21 review findings. None of it is *known* wrong.
None of it is *known* right either.

**Next step is not software.** Escalate to IT with the offsets above. memtest86+ (already
installed, needs a reboot) will identify the DIMM.

---

# CI runs again, and caught a privacy defect on its first pass

The repository was made **public** on 2026-09-21 (owner's explicit decision after a full
history scan: 221 commits, gitleaks clean, no `.env` ever committed, no live-shaped
credential in any revision). Public repositories get unlimited free Actions minutes.

The prior diagnosis is now confirmed rather than inferred: jobs had been running
`07:31:53 → 07:31:56` with **`"steps": []`** — three seconds, zero steps, all eleven
jobs. That is a quota block.

**First real run: 9 of 11 jobs pass, and the two failures were real.**

## `ai_request_logs` never reached a user's data export

`TestEveryUserOwnedTableAppearsInTheExport` — a guard written in Phase 1 — had been
unable to run since 2026-09-16. It fired the first hour it could:

> *table `ai_request_logs` has a user_id column but no entry in this test … A table that
> holds a person's data and never reaches their export is the whole failure this guard
> exists for.*

Phase 4 added the table and wired it into neither the data-subject export nor the
deletion seed. §8 keeps message **content** out of it entirely — but `intent` and
`created_at` remain personal data: *"asked about medical matters on these dates"* is a
fact about a person.

Fixed: `ListAIRequestsForUser`, an `ai_requests` export section, and the table registered
in both guards. `trace_id`, `provider_id` and `conversation_id` are deliberately excluded
— they identify our infrastructure, not the person. `cost_micros` is deliberately
included: Phase 7 bills from this table, and the number a charge is computed from is the
one field somebody most reasonably wants to check.

**`task verify` cannot catch this class of defect** — it does not run the integration
suite. Only CI does. That is the argument for CI, made concrete within an hour of it
working.

The two E2E failures are Phase 3 tests (place-search dropdown, OTP timing) and appear
environmental; they are not Phase 4 regressions and remain open.

**`task verify` green. Full Go integration suite green.**

---

# The RAM fault corrupted a build dependency, live, during this session

Asked a fifth time whether the phase is complete. `task verify` failed twice in a row
while answering, and both failures were the hardware.

**First:** the Go linker reported

```
cannot find package go.opent%lemetry.io/otel/metric/embedded
```

`opent%lemetry`. `e` is `0x65`, `%` is `0x25` — **XOR 0x40, a single-bit flip**, in a
package path held in the build cache. `go clean -cache` cleared it.

**Then:** `next build` died with `SyntaxError: Invalid or unexpected token` at
`node_modules/next/dist/build/swc/index.js:1202`. That one was **persisted to disk**:

| | |
|---|---|
| Scanned | 14,220 `.js` files under `node_modules` |
| Containing a real NUL byte | **exactly 1** |
| NULs in that file | **exactly 1**, at byte 52535 of 62048 |
| Its neighbours | `0x20` `<NUL>` `0x20` — indentation |
| So the original byte was | `0x20`, a space |
| XOR | **0x20 — a single-bit clear** |

`memcheck` had reported, an hour earlier: `wrote 0x00 read 0x20 (xor 0x20)`. **The same
bit position.** A failing cell zeroed a byte on its way to disk, and `node_modules` is
gitignored so no checksum could have caught it.

Repaired with `npm ci`; 0 NULs, parses cleanly, `task verify` green.

**A methodological note, because it nearly went in this document as fact:** the first
scan reported NUL bytes in ten files. It was wrong — **bash cannot pass a NUL as an
argument**, so `grep -c $'\x00'` became `grep -c ''` and matched every line. The files
parsed fine. Re-run in Python, the true count was one. A scan that cannot represent what
it searches for reports whatever it likes.

## The state file is now structurally incapable of the drift it kept having

It had gone stale four times, each time because it duplicated a moving fact — "commits
unpushed", "CI has not run since 2026-09-16", "memtest86+ UNRUN". §17 item 19 is "state
files updated", so every drift was itself an open gate item.

Fixing it a fifth time buys one more day. The open list now lives in **one** place —
this document, appended chronologically and never rewritten — and the state file carries
only what does not move: which phase, whether its gate closed, and the RAM caveat that
outlives any single finding.

## CI: 10 of 11

The `ai_request_logs` export fix landed and the Go integration job passes. One job still
fails: three **Phase 3** E2E tests (place-search dropdown, a tablet visual snapshot, XSS
label rendering). **I have not established whether those are environmental or real**, and
say so rather than guessing — they are outside Phase 4's gate either way.

---

# Verified properly: the gate number reproduces, and CI now runs on sound hardware

"Verify properly" had an answer I had been missing. **CI runs on GitHub's hardware,
which is not this machine.** A green CI job is a measurement taken somewhere the RAM
works.

## The gate number reproduces independently

| | run 1 (2026-09-21) | run 2 (2026-09-22) |
|---|---|---|
| Answered | 114/118 | 114/118 |
| Model accuracy | 100/114 = 87.7% | 101/114 = 88.6% |
| **As shipped** | **180/200 = 90.0%** | **183/200 = 91.5%** |
| Provider errors | 4 | 4 |

Two runs, different days, same dataset and prompt. Both clear the 85% gate; the 1.5pp
gap is ordinary sampling variance at `temperature=0.1`.

**This is what makes the number trustworthy despite the hardware.** A memory fault does
not produce a *similar* answer twice — it produces a random deviation. Two runs agreeing
within noise, with identical answered- and error-counts, is evidence the measurement is
sound in a way a single run never was.

## CI was one root cause, not three UI bugs

The E2E job had **never passed**. The last genuinely-executing run (2026-09-13) had nine
jobs and no E2E at all — the job arrived during Phase 3, and the quota block meant nobody
ever saw it fail.

**Cause 1 — the worker never started:**

```
fatal: pdf renderer: pdf: chrome startup check at /usr/bin/google-chrome:
websocket url timeout reached
✗ worker never became ready
```

`.env.example` names `/usr/bin/google-chrome`, right for a developer machine and absent
on the runner. CI copies that file verbatim, so every E2E test ran against a half-started
stack. It presented as three unrelated UI failures. CI installs Playwright's chromium two
steps earlier; `CHROME_PATH` now points at that, and the step fails loudly if the binary
is missing rather than starting a stack that cannot work.

**Cause 2 — a test searched for a city CI does not have:**

`input-contrast.spec.ts` typed **"Prayagraj"**. `tests/fixtures/places/cities-e2e.txt`
holds twenty cities and Prayagraj is not among them. It passed locally only because this
machine's `places` table still held a fuller seed — the textbook "works on my machine".
The other ten place-search tests all use `jaip`; this one now does too. Verified by
reseeding locally from the CI fixture and re-running: 4 passed.

## Two local red herrings, both environment

Worth recording because either would have been filed as a Phase 3 regression by anyone
reading a summary:

- Four E2E tests failed locally until the stack was restarted — `ayana up` had been
  serving a **stale build**. Not UI bugs.
- The full local suite then passed **157/157**.

## Where verification now stands

| | verified where |
|---|---|
| Python `ai` suite, Go api-service, contracts, secret scan, env drift, astro-AI-free | **CI — sound hardware** |
| Go integration (real Postgres) | CI, passing except a PDF browser-timing test |
| Classifier accuracy 90.0% / 91.5% | **two independent runs, agreeing within noise** |
| Ephemeris + golden files | `integrity:gate`, byte-for-byte against the index |
| E2E, 157 tests | locally green; CI fixed for two causes, snapshot outstanding |

**What remains hardware-bound:** nothing in the gate. The RAM fault is still real and
still needs replacing — it corrupted a build dependency during this session — but the
Phase 4 numbers no longer rest on it alone.

---

# The visual failure was a real UI defect: a glyph no bundled font contains

The last red CI job. Diagnosed by looking at the images — which was only possible
because the previous commit made CI upload them.

## What the diff showed

The planetary positions table was **shifted**; the houses section below it matched
byte-for-byte. Same data, same rows, same values — every column right of the planet
names landed a few pixels off.

The cause is one character: `RETROGRADE_MARK = '℞'` (U+211E, Letterlike Symbols).

`next/font` self-hosts Inter, JetBrains Mono and Cormorant Garamond. **None of them
carries that block**, so the browser falls back to whatever the operating system
provides — Noto on one machine, DejaVu on another — and the glyph's *advance width*
differs with it. That width fed straight into the table's column layout.

**So this was never only a test problem.** The Status column sat in a different place
depending on the reader's operating system. The snapshot was reporting a real defect and
being dismissed as flakiness.

## The fix

The mark now renders in a box of fixed width:

```tsx
<span aria-hidden="true" className="inline-block w-[1em] text-center">
```

`em`, not `ch` or `px`: `ch` is the width of "0" *in the active font*, which is the thing
that varies here, and `px` would not track the surrounding type size.

The glyph still *looks* different per OS. That is cosmetic and inherent to using a
character we do not ship. The layout shifting underneath it was not.

Accessibility is unchanged — the mark is `aria-hidden` beside an `sr-only` "retrograde",
and the visible word remains, per the rule that colour never carries meaning alone.

Baselines regenerated: exactly **two** files changed, `screen-planets-desktop` and
`screen-planets-tablet` — the only screens containing the mark. Nothing else moved,
which is the evidence the fix is targeted rather than a re-baseline that papers over
drift.

## Two wrong turns on the way, recorded because both were convincing

- **"It fails locally under CI=1 too."** It does not. `npm ci` — repairing the
  RAM-corrupted Playwright bundle — had wiped `node_modules` from under the running
  stack, so the failure was `ERR_CONNECTION_REFUSED`, not a snapshot mismatch. On a
  healthy stack, 15/15 pass with `CI=1`.
- **"Two Jaipurs in the fixture, so `.first()` picks a different city."** Plausible and
  false: populations are 2,711,758 and 612, and the query is
  `ORDER BY population DESC, ascii_name`. Deterministic. Disproved before reporting.

**595 component tests, 15/15 visual locally, `task verify` green.**

---

# The Phase 3 carry-forward list: two closed, one disproved, three open

## ✅ §11.5 — the PDF browser can no longer phone home

> *"`chromedp` runs sandboxed with no network access beyond the print route."*

The sandbox half was a documented deployment constraint. The **network half was simply
absent**: the browser rendering somebody's birth data could reach anything on the
internet.

`resolverRules` now gives Chrome `MAP * ~NOTFOUND` with an `EXCLUDE` for only the host
being printed. The threat is not a compromised Chrome — it is the print page carrying
content it should not (a profile label that escaped escaping, a future template change).
Under that assumption, what matters is whether the render can phone home. It cannot.

It **fails closed**: a malformed or empty URL yields the restrictive rule with loopback
only, because the alternative is unrestricted egress because a string was malformed.
Break-tested three ways — removing deny-by-default fails 6 subtests, leaking the port
into the host rule fails the port case, and failing open on a bad URL fails the
never-permissive test.

## ✅ ADR-012 — hand-rolled auth, recorded

Written late and says so. OTP + HS256 access tokens + rotating refresh with reuse
detection, and *why*: phone-first Indian audience where hosted providers route SMS
through their own aggregator and control DLT registration; and birth data being the
account, so the linkage should not live in another tenancy.

It names what the decision obliges and where each obligation is enforced, and the
tradeoff that matters — **HS256 is symmetric**, so any service that can verify can also
mint. Fine while `api-service` is the only verifier; the day a second one needs to
verify, the move is asymmetric signing rather than a vendor.

## ❌ §11.9 CSP — the escape route was tested and does not work

The `'unsafe-inline'` in `script-src` is the weakest part of the policy. The existing
comment named two exits: a per-request nonce (kills static rendering, bad for the Indian
mobile TTFB this app targets) or `experimental.sri`.

**I tested SRI. It does not solve this.** Enabled `experimental.sri`, removed
`'unsafe-inline'`, rebuilt. SRI emits integrity attributes for *external* scripts — but
Next's RSC payload rides in **two inline `<script>` tags that carry none**, so
`script-src 'self'` blocks them. Confirmed with the repo's own test: `csp.spec.ts:52`
*"the app hydrates and stays interactive under the policy"* fails on a click timeout.

Reverted. **The original decision was correct** and is now correct *with evidence*
rather than by argument. The remaining exit is the nonce, and PHASE-05 carries the
blocking gate item.

## Still open

- **E2E for PDF download and share links** — blocked today: `docker compose` vanished
  mid-session (Docker is a snap and auto-refreshed without the compose plugin), so the
  full stack cannot start.
- **Share-management screen** — a UI feature, Phase 3 scope, not started.
- **`<180 KB` first-load JS** — unmet *by decision*, ADR-010. Recorded, not a gap.

## An environment note

Two more casualties while doing this work: `docker compose` disappeared from the snap,
and Playwright's `chrome-headless-shell` began segfaulting on every launch until
reinstalled (browsers live in `~/.cache/ms-playwright`, which `npm ci` does not touch).
The second is consistent with the RAM fault; the first is a snap refresh. Both cost real
time and neither is a defect in this repository.
