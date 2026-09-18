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
