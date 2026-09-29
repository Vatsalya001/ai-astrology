# SeaweedFS S3 credentials (development only)

SeaweedFS's S3 gateway takes its identities from a JSON file rather than from
environment variables, which is the one place it differs operationally from the MinIO
setup it replaced. See [ADR-013](../../docs/decisions/013-object-storage-seaweedfs.md).

## Why the credentials are `minioadmin` / `minioadmin`

They are the well-known development defaults, unchanged from the MinIO setup so that
`.env.example`, every developer's `.env` and the CI workflow did not all have to move
at once for a swap that is about image availability, not about secrets.

**They are not secret and are not meant to be.** This file is committed deliberately:
it configures a container that binds to `127.0.0.1` only, holds synthetic fixtures, and
is never the store production uses — ADR-013 defers that choice to the deploying phase.

## Why the gateway is not left open instead

It would be one less file. It would also make
`TestPresignedURLsWorkAgainstTheRealGateway` meaningless: that test asserts a signed GET
returns 200 **and an unsigned GET returns 403**, and the second half is what proves the
presigned URL is doing the work rather than the bucket being world-readable.

A store with no credentials would pass the first assertion and fail the product — the
PDF download hands a browser a signed URL precisely so that possessing the URL is the
authorisation.

## Why the actions are bucket-scoped, and why that is not cosmetic

The identity has `Read:astro-dev` rather than `Read`, and no `Admin` at all. With the
unscoped actions this file first shipped with, **SeaweedFS created buckets on write** —
verified, not assumed: a PUT to `a-bucket-that-does-not-exist` succeeded and the bucket
appeared with 264 bytes in it.

MinIO refused that, and `storage.New` documents the reason in its own comment: *"a
bucket conjured by the first write hides a misconfigured bucket name behind a
working-looking service, and the first sign is objects nobody can find."* A typo in
`S3_BUCKET` would have produced a healthy-looking stack quietly writing PDFs into a
bucket nothing reads.

So the swap silently removed a safety property, and scoping the identity puts it back.
`TestTheBucketIsNotCreatedOnDemand` fails if it is ever widened.

`storage-init` is unaffected: it provisions the bucket through `weed shell`, which
talks to the master directly and never uses these S3 credentials.

## If you change the keys

Change them in three places, or the stack starts and the first upload fails:

1. this file
2. `S3_ACCESS_KEY` / `S3_SECRET_KEY` in `.env` (and `.env.example`)
3. the `storage-init` bucket step, if it ever needs authenticated calls — today it
   uses `weed shell`, which talks to the master directly and does not
   use the S3 credentials at all.
