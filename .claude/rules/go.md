# Go rules — services/api

## Boundaries
- A domain package imports `platform/` and `httpapi/`, never another domain's internals.
- Cross-domain access goes through an interface declared by the **consumer**, not the
  producer. This keeps the dependency graph acyclic and mocking trivial.
- `platform/` never imports a domain package.

## Errors
- Wrap with context: `fmt.Errorf("connect postgres: %w", err)`.
- Sentinel errors for conditions callers branch on; map to HTTP status in one place.
- Client-facing messages are generic. Detail goes to the log, keyed by trace ID.

## Context
- Every function doing I/O takes `context.Context` as its first parameter.
- Propagate it to downstream calls so client disconnects cancel work in progress.
- `context.WithoutCancel` when a write must survive a cancelled request (SSE persistence).

## Database
- `sqlc`, not an ORM (ADR-006). Write SQL in `db/queries/`, run `task sqlc`.
- Never `fmt.Sprintf` into SQL.
- Balance mutations: `SELECT … FOR UPDATE` inside a transaction. Always.

## Money
- `int64` paise. Never `float64`. Never `NUMERIC` through a float.
- Define distinct types (`type Paise int64`) so the compiler catches unit mix-ups.

## Testing
- `go test ./... -race -count=1`. `-race` is not optional.
- `testcontainers-go` for integration tests. Do not mock the database — the bugs that
  matter live in locking behaviour a mock cannot reproduce.
- Test the negative case. A guard never observed to fire is a guard you cannot trust.
