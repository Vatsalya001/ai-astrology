# ADR-013 — Object storage moves from MinIO to SeaweedFS

**Status:** accepted · 2026-09-29
**Forced, not chosen.** MinIO's images stopped being anonymously pullable and the
stack could no longer start on a cold machine. This records what was verified before
the swap, because the alternative to a real decision here is a green build on one
laptop and a red one everywhere else.

## Decision

`docker-compose.yml` runs **`chrislusf/seaweedfs`** as the S3-compatible object store
for development and CI. The application code does not change: `internal/platform/storage`
still talks to it through `minio-go`, which is an S3 client and was never tied to the
MinIO server.

## Context

The pinned image stopped resolving:

```
$ docker pull quay.io/minio/minio:RELEASE.2025-04-22T22-12-26Z
401 UNAUTHORIZED
```

Not transient, and not this network — `alpine:3` and `postgres:17-alpine` pulled fine
in the same session. Every path was tried:

| ref | result |
|---|---|
| `quay.io/minio/minio:RELEASE.2025-04-22T22-12-26Z` | 401 |
| `quay.io/minio/minio:latest` | failed |
| `minio/minio:latest` (Docker Hub) | pull access denied |
| `quay.io/minio/minio:RELEASE.2024-01-16…`, `…2023-09-04…` | failed |
| `bitnami/minio:latest` | failed |

This is the second such move. `docker-compose.yml` already carried a comment
explaining that Docker Hub's `minio/minio` had stopped being published, which is why
the image was on quay.io at all — and quay.io has now followed.

It had been working here only because the container predated the withdrawal. `docker
images` showed the pinned tag was not even in the local cache, so **a fresh
`docker compose up` could not have worked on any machine**, and the CI e2e job failed
on a commit that changed only Markdown.

## Why SeaweedFS

Three candidates were confirmed pullable in the same session. The requirement that
decided it is `storage.go:117`:

```go
signed, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, nil)
```

The PDF download hands a **browser** a presigned URL. A store that accepts writes and
rejects signed reads would pass a naive smoke test and break the one feature that uses
object storage.

| candidate | verdict |
|---|---|
| **SeaweedFS** | Apache-2.0, ~60 MB, starts in seconds, S3 gateway implements SigV4 presigning. Chosen |
| LocalStack | The most complete S3 emulation and by far the heaviest; adds real seconds to every CI e2e run, for fidelity this project does not need in development |
| `adobe/s3mock` | Smallest, but a *test double* rather than a server. `.claude/rules/testing.md` prefers real infrastructure in integration tests — "the bugs that matter live in behaviour a mock cannot reproduce" — and that argument applies to storage as much as to Postgres |

## What was verified before the swap, not after

Running `storage.Client` — the shipping one, not a reimplementation — against a real
SeaweedFS gateway:

```
signed GET   200, bytes match
unsigned GET 403
```

The second line is the control and matters as much as the first. Without it the test
would pass against a world-readable bucket, where presigning proves nothing. That check
is now `TestPresignedURLsWorkAgainstTheRealGateway`, behind the `integration` tag.

Also confirmed:

- **Bucket provisioning needs no second image.** `weed shell` creates it from the same
  container: `s3.bucket.create -name astro-dev`. The old setup needed
  `quay.io/minio/mc`, which carries exactly the withdrawal risk that caused this ADR.
- **Health check** is `wget -q -O /dev/null http://127.0.0.1:8333/status`. Two details,
  both found by the check failing rather than by reading docs: `curl` is in `:latest`
  and **not** in `:3.97`, which is what is pinned — probing one image while pinning
  another is how it first shipped broken — and `localhost` resolves to `::1` under
  busybox while SeaweedFS binds IPv4 only, so the check reported "connection refused"
  against a server that was up and answering. MinIO's `mc ready local` had the same
  second-image problem this avoids.

## The swap removed a safety property, and it had to be put back

SeaweedFS **creates buckets on write** where MinIO refused. A PUT to
`a-bucket-that-does-not-exist` succeeded and the bucket appeared holding 264 bytes.

`storage.New` deliberately does not create buckets, and says why: *"a bucket conjured
by the first write hides a misconfigured bucket name behind a working-looking service,
and the first sign is objects nobody can find."* A typo in `S3_BUCKET` would have given
a healthy stack writing PDFs nothing would ever read.

Fixed by scoping the dev identity — `Read:astro-dev` rather than `Read`, and no
`Admin` — so the gateway refuses to create anything. `TestTheBucketIsNotCreatedOnDemand`
fails if that is ever widened.

This is the part of the ADR worth re-reading before adopting any other S3 substitute:
the interesting differences between implementations are not in the operations they
support, but in what they do when asked for something that should not exist.

## Tradeoffs

- **No web console.** MinIO's console on `:9001` is gone; SeaweedFS's filer UI is a
  file browser, not an object-store admin panel. It is exposed in its place and
  described as what it is rather than as a replacement.
- **Less S3 surface.** SeaweedFS implements the common operations, not all of S3. This
  service uses three — `PutObject`, `PresignedGetObject`, `MakeBucket` — so the gap is
  theoretical today and would be discovered the first time it is not.
- **Different failure modes under load.** Unknown, and untested here. Development and
  CI are the only environments this ADR covers.

## Scope

**Development and CI only.** Production object storage is not decided by this ADR and
should be a managed S3 (or S3-compatible) service chosen in the phase that deploys —
the same deferral ADR-011 made for the production model provider, for the same reason:
picking infrastructure for an environment that does not exist yet is guesswork that
later reads as a decision.

`S3_PATH_STYLE` stays `true` for SeaweedFS and would become `false` against AWS S3
proper. The application already reads it from config.

## Revisit when

- Production deployment is designed — this ADR does not cover it.
- Any S3 operation beyond the current three is needed; check SeaweedFS implements it
  **by executing it**, not by reading its compatibility table.
- MinIO's images become freely available again. That would not on its own justify
  moving back: the cost of this swap is already paid, and a second migration for
  symmetry would be churn.
