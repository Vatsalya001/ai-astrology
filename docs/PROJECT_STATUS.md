# Project Status

**Updated:** 2026-09-13
**Current phase:** 0 — Foundation
**Gate:** ✅ **CLOSED** — 19 of 20 met; 1 blocked by a hardware fault, documented below
**Repo:** https://github.com/Vatsalya001/ai-astrology (private)

---

## Phase 0 gate

Evaluated against the running system, not against intent.

| # | Item | State |
|---|---|---|
| 1 | `docker compose up -d` brings up all six services healthy | ✅ |
| 2 | Ollama models: `llama3.2:3b`, `qwen2.5:7b`, `nomic-embed-text` | ⛔ **blocked** — see §Hardware |
| 3 | `task verify` passes | ✅ |
| 4 | `task dev` starts web, API, astro, ai | ✅ |
| 5 | Status page shows six dependencies | ✅ |
| 6 | `task migrate` applies; extensions enabled | ✅ up **and** down verified |
| 7 | `task sqlc` generates compiling Go | ✅ |
| 8 | `task contracts` idempotent; CI diff check | ✅ |
| 9 | Go calls Python via **generated** clients | ✅ hand-written client deleted |
| 10 | One request → correlated `trace_id` in all three services | ✅ |
| 11 | Missing env var → named startup failure | ✅ |
| 12 | PII redaction in Go **and** Python | ✅ |
| 13 | `astro_ro` cannot write — **asserted in a test** | ✅ |
| 14 | `astro-service` has no LLM dependency — CI rule | ✅ |
| 15 | CI green across all jobs | ✅ 8 jobs |
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
| Secret scan clean | gitleaks over full history and working tree |

**Tests:** Go 2 unit suites + 1 integration suite (all `-race`); Python 76.
`mypy --strict` and `ruff` clean on both services.

---

## ⛔ Hardware fault — this needs your attention

**Five data-corruption events occurred during this session.** This is not normal and it
will corrupt your work, not just this project.

| # | What | Evidence |
|---|---|---|
| 1 | Go stdlib `src/time/time.go` | `0x09` → `0x08` — single bit flip. Broke every build importing `time`. |
| 2 | uv cache, mypy typeshed `inspect.pyi` | `0x70` → `0x60`, `Mapping` → `` Ma`ping ``. Broke `mypy --strict`. |
| 3 | Ollama `qwen2.5:7b` (4.7 GB) | Checksum failed **twice with two different wrong hashes** (`ce342d11…`, then `ade8f13f…`) |
| 4 | Go build cache, `go/types` | `0x6E` → `0x6A`, `constDecl` → `cojstDecl`. Broke linking. |
| 5 | `next-swc.linux-x64-gnu.node` | Corrupted native binary → deterministic segfault on every `next build`. Fixed by reinstalling. |

Four bit flips at **four different bit positions**, across four unrelated tools. Two
different corrupt results for the same download rules out a bad mirror or a bad cache.

**What the evidence says:** every event involved data arriving over the network and
being written to disk. Kernel logs show no MCE, no disk errors, and ECC counters read
zero — but the machine uses Wi-Fi (`wlp0s20f3`) with no VPN active, and the NIC's own
error counters are clean, so the corruption is happening past the driver.

**Suggested next steps, cheapest first:**

1. Retry the 4.7 GB download over **ethernet** rather than Wi-Fi. If it succeeds, the
   problem is the wireless path. This is the decisive test.
2. `sudo memtest86+` (or boot the memtest entry) — an overnight run.
3. `sudo smartctl -a /dev/nvme0n1` for disk health.

Until then, **gate item 2 is blocked by the environment, not by the code.**
`nomic-embed-text` (274 MB) downloaded fine; only the large file fails. Neither model
is used by any code until Phase 4, so this does not block Phase 1.

---

## Other environment issues, resolved

| Issue | Resolution |
|---|---|
| Ports 5432 / 6379 / 6380 already in use | Postgres → **5433**, Redis → **6381** |
| `corepack enable` needs root | npm workspaces (ADR-008) |
| Go 1.27.1 breaks `govulncheck` | Pin one minor behind: `go1.26.8` (ADR-007) |
| System Go 1.22 stdlib corrupted | Toolchain pin sidesteps it. Optional host repair: `sudo apt-get install --reinstall golang-1.22-src` |

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
