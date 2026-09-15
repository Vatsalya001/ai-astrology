# Project Status

**Updated:** 2026-09-14
**Current phase:** 0 — Foundation
**Gate:** ✅ **CLOSED — 20 of 20**
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

| ADR | Question | Due |
|---|---|---|
| [003](decisions/003-astrology-engine.md) | Swiss Ephemeris licence: AGPL, commercial, or MIT `skyfield` | **Before Phase 7** |

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

## Next

**Phase 2 — Astrology Engine.** `docs/specs/PHASE-02-ASTROLOGY-ENGINE.md`.

Entirely Python, in `services/astro`. The Go service gains a generated client and
nothing else. Two things decide whether this phase is any good:

1. **Cross-validate every golden file against an independent reference before freezing
   it.** A golden file that encodes your own bug makes that bug permanent.
2. **`Decimal`, not float, for dasha arithmetic.** Error accumulates across three
   levels of subdivision and produces dates wrong by days.
