module github.com/Vatsalya001/ai-astrology/services/api

go 1.26.0

// Pinned deliberately. The system Go 1.22.2 install on at least one dev
// machine has a corrupted stdlib file (a single flipped bit in
// src/time/time.go), which breaks every build that imports "time".
// Naming a toolchain here makes the Go tool fetch a verified one into
// the module cache, so builds are reproducible regardless of the state
// of the system install. See docs/decisions/007-go-toolchain-pin.md.
toolchain go1.26.8

require (
	github.com/caarlos0/env/v11 v11.2.2
	github.com/go-chi/chi/v5 v5.3.2
	github.com/go-chi/cors v1.2.1
	github.com/go-playground/validator/v10 v10.30.4
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/redis/go-redis/v9 v9.22.0
	golang.org/x/sync v0.23.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
