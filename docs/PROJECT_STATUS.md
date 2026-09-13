# Project Status

**Updated:** 2026-09-13
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

## Open decisions

| ADR | Question | Due |
|---|---|---|
| [003](decisions/003-astrology-engine.md) | Swiss Ephemeris licence: AGPL, commercial, or MIT `skyfield` | **Before Phase 7** |

---

## Next

**Phase 1 — Authentication & User Profiles.** `docs/specs/PHASE-01-AUTH-AND-USERS.md`.

First decision in that phase: managed auth versus hand-rolled. Hand-rolled auth is a
classic source of vulnerabilities; decide before building, not after.
