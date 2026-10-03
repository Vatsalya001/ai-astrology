# The astrology knowledge base

Authored source for retrieval. `services/api/cmd/ingest-kb` reads this directory,
chunks it, embeds it through `ai-service`, and writes it to `knowledge_documents` and
`knowledge_chunks`.

This file is skipped by the ingester, along with anything whose name starts with `_`.
Documentation *about* the corpus must not end up in the retrieval index — it gets
quoted back at a user eventually.

---

## File format

JSON front matter between `---` fences, then markdown.

```markdown
---
{
  "title": "Saturn in the Tenth House",
  "category": "houses",
  "source": "editorial",
  "authority": 70,
  "metadata": {
    "planet": "saturn",
    "house": 10,
    "topic": ["career", "authority"]
  }
}
---

Prose, in sections.
```

JSON rather than YAML deliberately, and the reasoning is in the package doc of
`services/api/internal/knowledge/document.go`. The short version: `metadata` goes
into a JSONB column verbatim, and YAML would turn `language: no` into `false`.

### Fields

| Field | Required | Default | Notes |
|---|---|---|---|
| `title` | yes | — | Prepended to every chunk. Part of the document's identity. |
| `category` | yes | — | `planets`, `signs`, `houses`, `nakshatras`, `dashas`, `yogas`, `transits`, `aspects`, `remedies`, `career`, `marriage`, … |
| `source` | yes | — | Provenance. See licensing below. |
| `authority` | no | `50` | 0–100. Classical texts outrank editorial paraphrase. |
| `language` | no | `en` | Lowercase. Phase 5 ships `en` only. |
| `astrology_system` | no | `vedic` | |
| `version` | no | `1` | Part of the identity: `(title, language, version)` is unique. |
| `metadata` | no | `{}` | The retrieval filter surface. |

An unknown field is an error, not a warning. A typo'd key is a filter that silently
does not exist, so the document never matches the query it was written for.

### `metadata` is the filter surface

Retrieval narrows by these before it ranks, using `metadata @> '{"planet":"saturn"}'`
against a GIN index. Keys used today:

- `planet` — lowercase: `saturn`, `rahu`, `ketu`
- `house` — integer 1–12
- `sign` — lowercase: `capricorn`
- `nakshatra` — lowercase: `rohini`
- `topic` — array of lowercase strings

Lowercase everywhere, because containment is an exact match. `"Saturn"` and
`"saturn"` are two different documents to Postgres and only one of them is reachable.

The chunker adds `_section`, the markdown heading a chunk came from. The underscore
keeps it from colliding with anything you author.

---

## Writing for retrieval

**One idea per document.** "Saturn in the 10th house" is a document. "Saturn" is a
book. The retriever returns chunks, and a chunk from a sprawling document is a chunk
whose neighbours are about something else.

**Use sections.** A markdown heading is a hard chunk boundary — nothing is ever
chunked across one, and no overlap crosses one. Sections are how you control what
ends up adjacent to what.

**Traditional framing, never predictive.** "is traditionally read as", not "you
will". `.claude/rules/ai.md` is the rule and the validator enforces it on output,
but output inherits its voice from here.

**No guaranteed outcomes** — medical, marriage, pregnancy, death, legal, financial.

**Do not write placements.** This corpus holds *interpretation*; positions come from
`astro-service`. A document asserting where Saturn is in 2026 is a fact the model
will repeat and the `fact_index` will then block.

---

## Licensing

PHASE-05 §4. Public domain or own-authored only.

| Source | Status |
|---|---|
| Classical Sanskrit texts (BPHS, Phaladeepika, Saravali) | Public domain — **but verify the translation edition** |
| Public-domain English translations | Free |
| Own editorial content | Free |
| Modern astrology books | ❌ Copyrighted |
| Scraped websites | ❌ Copyrighted, and poor quality |

`source` is required with no default for this reason. A corpus where provenance is
optional cannot be audited for licence cleanliness after the fact.

---

## Checking your work

```bash
cd services/api
go run ./cmd/ingest-kb --check            # parse + chunk, write nothing
go run ./cmd/ingest-kb --check --print 3  # ...and show three chunks in full
```

No database, no model, no network. Errors name the file and the field.
