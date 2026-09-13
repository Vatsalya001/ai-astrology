# Current phase

```
Phase: 0 — Foundation
Gate:  🔒 LOCKED  (5 items outstanding)

├── 0.1  Repo skeleton + Taskfile          ✅
├── 0.2  Docker Compose + astro_ro role    ✅
├── 0.3  Ollama free models                ⏳  (task ollama — not needed until Phase 4)
├── 0.4  Go config, fail-fast              ✅
├── 0.5  Go router, health, logging, trace ✅
├── 0.6  migrate + sqlc wired              🟡  tools installed, no migrations yet
├── 0.7  astro-service + AI-free guard     ✅
├── 0.8  ai-service + startup guards       ✅
├── 0.9  Contract pipeline                 ⏳  deferred to Phase 1
├── 0.10 Go → Python typed clients         ✅
├── 0.11 Web: tokens, landing, status      ✅
├── 0.12 packages/* stubs                  ⏳
├── 0.13 Test harness                      ✅  Go 3 suites, Python 20 tests
├── 0.14 CI pipeline                       🟡  written, not yet run on a PR
├── 0.15 .claude/ harness                  ✅
├── 0.16 Docs + ADRs                       ✅  8 ADRs
└── 0.17 Synthetic fixtures                ⏳
```

Next task: 0.17 — write 10 synthetic birth profiles in `tests/fixtures/charts/`.

Do not begin Phase 1 until every box above is ✅ and the Phase Gate in
`docs/specs/PHASE-00-FOUNDATION.md` passes.
