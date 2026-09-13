# api-service — the HTTP API and background worker.
#
# The only service exposed to the internet, and the sole writer to the
# database.
#
# Two binaries are built from one image. cmd/worker is separate from
# cmd/api because their workloads diverge later: Phase 3's PDF renderer
# needs headless Chromium, and the API image must stay small.

# ─── Build ───────────────────────────────────────────────────────────
# The builder tag must satisfy the `toolchain` directive in go.mod
# (go1.26.8). See ADR-007 for why that is pinned.
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Dependencies first, so a source change does not re-download them.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev

# CGO_ENABLED=0 produces a static binary, which is what makes the
# distroless runtime image below possible.
# -trimpath keeps build paths out of the binary; -s -w strip symbols.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X github.com/Vatsalya001/ai-astrology/services/api/internal/httpapi.Version=${VERSION}" \
        -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X github.com/Vatsalya001/ai-astrology/services/api/internal/httpapi.Version=${VERSION}" \
        -o /out/worker ./cmd/worker

# ─── Runtime ─────────────────────────────────────────────────────────
# distroless: no shell, no package manager, no busybox. If someone gets
# code execution in this container there is nothing here to pivot with.
FROM gcr.io/distroless/static-debian12:nonroot AS api

COPY --from=builder /out/api /api

EXPOSE 4000
USER nonroot:nonroot

# No HEALTHCHECK: distroless has no shell and no curl. Health is probed
# externally — by compose, or by the orchestrator hitting GET /health.
ENTRYPOINT ["/api"]

# ─── Worker ──────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS worker

COPY --from=builder /out/worker /worker

USER nonroot:nonroot
ENTRYPOINT ["/worker"]
