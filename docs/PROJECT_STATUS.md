# Project Status

**Updated:** 2026-09-13
**Current phase:** 0 — Foundation
**Gate:** 🟡 Mostly passing — see open items

---

## Phase 0 task board

| # | Task | State |
|---|---|---|
| 0.1 | Repo skeleton, Taskfile, npm workspace, go.mod, two pyproject.toml | ✅ |
| 0.2 | Docker Compose + DB bootstrap with `astro_ro` role | ✅ |
| 0.3 | Ollama + free models pulled | ⏳ not yet run (`task ollama`) |
| 0.4 | Go config with fail-fast validation | ✅ |
| 0.5 | Go: chi router, `/health` fan-out, slog + PII redaction, trace-id, error middleware | ✅ |
| 0.6 | `golang-migrate` + `sqlc` wired | 🟡 tools installed; no migrations yet |
| 0.7 | Python astro skeleton + AI-free import guard | ✅ |
| 0.8 | Python ai skeleton + startup guards | ✅ |
| 0.9 | Contract pipeline (OpenAPI → generated Go clients) | ⏳ deferred to Phase 1 |
| 0.10 | Go → astro/ai clients with timeout + trace propagation | ✅ |
| 0.11 | Next.js skeleton, tokens, landing + status page | ✅ |
| 0.12 | `packages/*` stubs | ⏳ directories exist, empty |
| 0.13 | Test harness | 🟡 Python done; Go tests not yet written |
| 0.14 | CI pipeline | ⏳ |
| 0.15 | `.claude/` harness | ⏳ |
| 0.16 | Docs + ADRs | ✅ 8 ADRs written |
| 0.17 | 10 synthetic birth-profile fixtures | ⏳ |

---

## Verified working

Each of these was demonstrated on a running system, not merely written.

| Property | Evidence |
|---|---|
| All four containers healthy | `docker compose ps` — postgres, redis, minio, mailpit |
| `pgvector` + `pg_trgm` installed | `SELECT extname FROM pg_extension` |
| **Single-writer rule enforced** | `astro_ro` `INSERT` → `ERROR: permission denied`; `SELECT` → works |
| Go API builds, vets and formats clean | `go build ./...`, `go vet ./...`, `gofmt -l` empty |
| Aggregate health fan-out | `/health` reports postgres 1ms, redis 0ms, astro 2ms, ai 2ms |
| **Degraded ≠ down** | astro stopped → HTTP 200, `"status":"degraded"`, others `ok` |
| Trace ID propagation | Client `X-Trace-Id` honoured, echoed, and present in log lines |
| Structured JSON logs | `{"level":"INFO","service":"api","trace_id":"…"}` |
| **PII guard fires in production config** | `ENV=production` + free tier → `UnsafeConfigurationError`, startup aborted |
| astro-service is AI-free | 4 tests: no LLM SDK imports, no HTTP clients, none installed |
| ai-service guards | 16 tests covering every disallowed provider/env combination |
| Web builds and renders | `/` static, `/status` dynamic; 0 npm vulnerabilities |

**Test totals:** Python 20 passing (4 astro + 16 ai). Go: 0 — not yet written.

---

## Open items before the Phase 0 gate closes

1. **Go tests** — config validation, PII redaction, error mapping, health aggregation.
   The largest gap: Go currently has no test coverage at all.
2. **CI pipeline** — five jobs (go, python matrix, web, contracts, e2e).
3. **`.claude/` control layer** — rules, agents, workflows, state.
4. **Synthetic fixtures** — 10 invented birth profiles.
5. **Contract pipeline** — deferred to Phase 1, when there is an actual domain endpoint
   to generate a client for. Generating a client for `/health` alone would be
   ceremony without value.
6. **Ollama models** — `task ollama`. Not needed until Phase 4.

---

## Known environment issues

| Issue | Impact | Resolution |
|---|---|---|
| System Go 1.22 stdlib has a corrupted byte in `src/time/time.go` | Would break every build | Worked around by pinning `toolchain go1.23.4` in go.mod (ADR-007). Optional host repair: `sudo apt-get install --reinstall golang-1.22-src` |
| Ports 5432, 6379 and 6380 already in use on this machine | Container bind failures | Postgres → **5433**, Redis → **6381** |
| `corepack enable` needs root | pnpm unavailable | Using npm workspaces (ADR-008) |

---

## Open decisions

| ADR | Question | Due |
|---|---|---|
| [003](decisions/003-astrology-engine.md) | Swiss Ephemeris licence: AGPL, commercial, or MIT `skyfield` | **Before Phase 7** |

---

## Next

Close the five open items above, then Phase 1 — Authentication & User Profiles.
Nothing in Phase 1 may start until the Phase 0 gate in
`docs/specs/PHASE-00-FOUNDATION.md` passes in full.
