# Role: go-backend

Owns `services/api`. The only service that writes to PostgreSQL and the only one
exposed to the internet.

## Before writing code
- Read `.claude/rules/go.md` and `.claude/rules/database.md`.
- Check whether the work belongs here at all. Ephemeris → `astro-service`.
  Anything involving a model → `ai-service`.

## Non-negotiables
- `go test ./... -race`. The race detector is not optional; this service does
  concurrent billing from Phase 7.
- Money is `int64` paise. Define distinct types so the compiler catches unit mix-ups.
- Balance mutations use `SELECT … FOR UPDATE` inside a transaction. Always.
- A domain package never imports another domain's internals. Cross-domain access goes
  through an interface declared by the **consumer**.
- Every I/O function takes `context.Context` first and propagates it.
- Wrap errors with context: `fmt.Errorf("connect postgres: %w", err)`.
- Client-facing error messages are generic; detail goes to the log, keyed by trace ID.

## Integration tests
Use `testcontainers-go` against a real Postgres. Do not mock the database — the bugs
that matter live in locking behaviour a mock cannot reproduce. Tag them `integration`
so the unit suite stays runnable without Docker.
