# Project Status

**Updated:** 2026-09-13
**Current phase:** 0 — Foundation
**Gate:** 🟡 Nearly closed — 4 items remain, none blocking
**Repo:** https://github.com/Vatsalya001/ai-astrology (private)
**CI:** ✅ all 6 jobs green on `main`

---

## Phase 0 task board

| # | Task | State |
|---|---|---|
| 0.1 | Repo skeleton, Taskfile, npm workspace, go.mod, two pyproject.toml | ✅ |
| 0.2 | Docker Compose + DB bootstrap with `astro_ro` role | ✅ |
| 0.3 | Ollama + free models pulled | ⏳ `task ollama` — not needed until Phase 4 |
| 0.4 | Go config with fail-fast validation | ✅ |
| 0.5 | Go: chi router, `/health` fan-out, slog + PII redaction, trace-id, error middleware | ✅ |
| 0.6 | `golang-migrate` + `sqlc` wired | 🟡 tools installed; first migration lands in Phase 1 |
| 0.7 | Python astro skeleton + AI-free import guard | ✅ |
| 0.8 | Python ai skeleton + startup guards | ✅ |
| 0.9 | Contract pipeline (OpenAPI → generated Go clients) | ⏳ deferred to Phase 1 |
| 0.10 | Go → astro/ai clients with timeout + trace propagation | ✅ |
| 0.11 | Next.js skeleton, tokens, landing + status page | ✅ |
| 0.12 | `packages/*` stubs | ⏳ directories exist, empty |
| 0.13 | Test harness | ✅ Go 2 suites (`-race`), Python 20 tests |
| 0.14 | CI pipeline | ✅ 6 jobs, green on `main` |
| 0.15 | `.claude/` harness | ✅ |
| 0.16 | Docs + ADRs | ✅ 8 ADRs |
| 0.17 | Synthetic birth-profile fixtures | ✅ 12 profiles with edge-case coverage |

---

## Verified working

Each demonstrated on a running system, not merely written.

| Property | Evidence |
|---|---|
| All four containers healthy | postgres, redis, minio, mailpit |
| `pgvector` + `pg_trgm` installed | `SELECT extname FROM pg_extension` |
| **Single-writer rule enforced** | `astro_ro` `INSERT`/`DELETE` → `permission denied`; `SELECT` → works |
| Go builds, vets, formats clean | `go build`, `go vet`, `gofmt -l` empty |
| **0 Go vulnerabilities** | `govulncheck` — down from 33 |
| **0 npm vulnerabilities** | `npm audit` after Next.js 15.1.6 → 16.3.5 |
| Aggregate health fan-out | postgres 1ms · redis 0ms · astro 2ms · ai 2ms |
| **Degraded ≠ down** | astro stopped → HTTP 200 `"degraded"`, others `ok` |
| Trace ID propagation | client `X-Trace-Id` honoured, echoed, present in logs |
| **PII redaction** | 15 sensitive keys incl. birth date/time/place; survives derived loggers |
| **PII guard aborts production startup** | `ENV=production` + free tier → `UnsafeConfigurationError` |
| astro-service is AI-free | AST scan + installed-package check, as a named CI job |
| Web builds and renders | `/` static, `/status` dynamic with live API data |

**Tests:** Go 2 suites under `-race`, Python 20. `mypy --strict` and `ruff` clean.

---

## Remaining before the gate closes

None blocking; all four are cheap and can be done alongside Phase 1 planning.

1. **`packages/*` stubs** (0.12) — directories exist but are empty. Phase 10 depends on
   `packages/astrology-geometry` being React-free, so the shape matters more than the
   content right now.
2. **Contract pipeline** (0.9) — deliberately deferred. Generating a typed client for
   `/health` alone is ceremony; Phase 1 gives it a real endpoint to generate from.
3. **First migration + sqlc output** (0.6) — Phase 1 creates the `users` table.
4. **Ollama models** (0.3) — one command, not needed until Phase 4.

---

## Environment issues found and handled

| Issue | Impact | Resolution |
|---|---|---|
| System Go 1.22 stdlib had a flipped bit in `src/time/time.go` (`0x09`→`0x08`) | Broke every build importing `time` | Pinned `toolchain go1.26.8` (ADR-007). Host repair is optional: `sudo apt-get install --reinstall golang-1.22-src` |
| uv cache had a flipped bit in mypy's typeshed (`0x70`→`0x60`, `Mapping`→`` Ma`ping ``) | `mypy --strict` failed with a syntax error in a vendored stub | Purged the cache entry and re-fetched. Clean afterwards, so transient rather than persistent. |
| Ports 5432 / 6379 / 6380 already in use | Container bind failures | Postgres → **5433**, Redis → **6381** |
| `corepack enable` needs root | pnpm unavailable | npm workspaces (ADR-008) |
| Go 1.27.1 breaks `govulncheck` | Parse errors instead of a scan | Pin one minor behind: 1.26.8 (ADR-007) |

> **Two independent single-bit corruptions in one session** is unusual. The second was
> transient and cleared on re-fetch, so this is probably coincidence rather than failing
> hardware — but if a third appears, run `memtest86+` and `smartctl -a`.

---

## Open decisions

| ADR | Question | Due |
|---|---|---|
| [003](decisions/003-astrology-engine.md) | Swiss Ephemeris licence: AGPL, commercial, or MIT `skyfield` | **Before Phase 7** |

---

## Next

Close the four remaining items, then **Phase 1 — Authentication & User Profiles**.

Nothing in Phase 1 may start until the Phase 0 gate in
`docs/specs/PHASE-00-FOUNDATION.md` passes in full.
