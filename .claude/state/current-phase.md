# Current phase

```
Phase: 1 — Authentication & User Profiles
Gate:  ✅ CLOSED  (16 of 17 gate items + §10, §11, §13 checklists)
       1 item open by decision — Google OAuth needs credentials
```

Evidence for every item: `docs/TESTING-PHASE-1.md`. It records what was *run*, not
what was read, and it names the guards that were deliberately broken to prove they
fire.

**The lesson from Phase 0 held again, and harder.** Reading the code said rate
limiting was complete. Running it found `POST /users/me/challenge` accepting 25 of 25
calls and sending 25 emails; `GlobalPerIP` declared as "the backstop, applies to every
request" and wired to nothing; and `go test -tags=integration` reporting `ok` while
skipping every test because Docker was unavailable. None of that was visible on the
page. Execute the check.

## Open by decision, not omission

**Google OAuth (gate item 3).** No credentials exist; this is the account owner's
call. The flow is built and fully covered against a stubbed Google with a real
Redis — state forgery, replay, expiry, 16-way concurrency, unverified email. Turning
it on is two lines in `.env`:

```
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

Authorised redirect URI: `http://localhost:4000/api/v1/auth/oauth/google/callback`.
The button then appears by itself — the web app asks `GET /api/v1/auth/providers`
rather than keeping a second copy of the credential state.

## Next: Phase 2 — Astrology Engine

Spec: `docs/specs/PHASE-02-ASTROLOGY-ENGINE.md`

Entirely a Python phase in `services/astro`. The Go service gains a client, nothing
more.

**Read the spec end to end first.** The task list, module layout and golden-file
strategy are already written; do not redesign them ad hoc.

**Carried into Phase 2:**

- **ADR-003, the Swiss Ephemeris licence, must close before Phase 7** — AGPL versus
  commercial versus MIT `skyfield`. It is not a Phase 2 blocker, but Phase 2 is where
  the dependency actually gets chosen, so deciding late means rewriting maths.
- `astro-service` has no model SDK, no API key and no HTTP client.
  `tests/test_no_llm_imports.py` fails the build if one appears. Do not add one.
- `app/core/` stays pure: no I/O, no database, no ambient clock. "Now" is passed in.
  That purity is what makes golden-file testing possible.
- Use `Decimal` for dasha arithmetic. Float error accumulates across three levels of
  subdivision and produces dates wrong by days.
- **Cross-validate every golden file against an independent reference before freezing
  it.** A golden file that encodes your own bug makes that bug permanent.
- The deletion integration test extends with every new user-owned table. Phase 2 adds
  `birth_profiles` and `charts`; both belong in it before the phase closes.

**Carried debt:**

- The web CSP still carries `script-src 'unsafe-inline'`, because Next's App Router
  cannot hydrate without it and the alternatives cost static rendering or need an ADR.
  Acceptable while the app renders no untrusted content. **PHASE-05 carries a blocking
  gate item** to replace it before the chat renders its first model response.
- This machine has produced **nine** data-corruption events (a Go linker panic, a
  Turbopack cache checksum mismatch, others in `docs/PROJECT_STATUS.md`). `memtest86+`
  is still unrun. Before chasing a mysterious build failure, clear the relevant cache
  and try again.
