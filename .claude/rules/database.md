# Database rules

## The single-writer rule
Only `api-service` writes. `ai-service` is `astro_ro` (SELECT only, enforced by grants).
`astro-service` has no access. When Python needs data persisted, it returns it.

## Migrations
- `golang-migrate`, owned by `api-service`. Python services never touch schema.
- **Every migration has a real `down`.** "Revert the commit" is not a rollback plan.
- Destructive changes go through shadow mode: add column → dual-write → parity check →
  only then drop the old one.

## Conventions
- `snake_case` tables and columns. Plural table names.
- UUID primary keys (`gen_random_uuid()`).
- `TIMESTAMPTZ`, never `TIMESTAMP`.
- Money is `BIGINT` paise. Never `NUMERIC`, never float.
- Index every foreign key and every column in a `WHERE` or `ORDER BY`.

## Cache keys
Must include **every input that affects the output**. Omitting `ayanamsa` from a chart
cache key is the classic bug: change a preference, get someone else's chart back.

Never cache PII. Order IDs and config are safe; names, emails and birth details are not.
