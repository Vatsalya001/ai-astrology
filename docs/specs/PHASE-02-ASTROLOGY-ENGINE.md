# Phase 2 — Birth Profiles & the Astrology Engine

| | |
|---|---|
| **Goal** | Turn birth details into a correct, reproducible, structured chart. No AI anywhere in this phase. |
| **Deliverable** | `astro-service` (Python) computes charts, dashas and transits deterministically; `api-service` (Go) owns birth profiles, persists charts and caches them. |
| **Depends on** | Phase 1 |
| **Unlocks** | Phase 3 |
| **Estimated size** | 12–18 days — the largest and most correctness-critical phase in the MVP |
| **Cost to run** | ₹0 — the ephemeris and the geocoding dataset are both free |

> This is the foundation everything else stands on. If the chart is wrong, every AI
> response is confidently wrong, every report is wrong, and no amount of prompt
> engineering fixes it. Golden-file tests are not optional here.

---

## 1. Scope

### In scope

**`astro-service` (Python)** — stateless, no database, no model access:
- Swiss Ephemeris via `pyswisseph`
- Rasi (D1): ascendant, 12 houses, 9 grahas, signs, degrees, nakshatras + padas
- Retrograde, combustion, exaltation/debilitation, dignity
- Navamsa (D9); D10 optional
- Vimshottari Dasha tree — Maha / Antar / Pratyantar
- Planetary aspects (graha drishti)
- Transits (gochara), incl. Sade Sati
- Yoga detection — a small, well-tested set

**`api-service` (Go)**:
- Birth profile capture, versioned (never overwritten)
- Place search + coordinate resolution from a self-hosted free dataset
- Chart persistence, caching and recomputation
- Ownership enforcement on every route
- Daily transit refresh worker

### Out of scope
- Any UI (Phase 3)
- Any interpretation, any LLM (Phase 4+)
- Compatibility / Ashtakoota (Phase 6, in `astro-service`)
- Western/tropical charts — the data model accommodates them; the engine doesn't yet

---

## 2. Architecture

```
Client
  │  POST /api/v1/birth-profiles
  ▼
api-service (Go)
  │  validate, resolve place, resolve historical timezone → utc_instant
  │  persist birth_profiles row (versioned, immutable)
  │  check chart cache (Redis → Postgres)
  │           │ miss
  │           ▼
  │  POST http://astro:8100/v1/charts/compute        ← generated typed client
  │           │
  │           ▼
  │   ┌─────────────────────────────────────────────┐
  │   │  astro-service (Python) — PURE, STATELESS    │
  │   │                                              │
  │   │  app/core/ephemeris.py   julian day, planets │
  │   │  app/core/chart.py       ascendant, houses   │
  │   │  app/core/nakshatra.py   nakshatra, pada     │
  │   │  app/core/varga.py       D9, D10             │
  │   │  app/core/dasha.py       Vimshottari tree    │
  │   │  app/core/aspects.py     graha drishti       │
  │   │  app/core/yoga.py        yoga predicates     │
  │   │  app/core/transit.py     gochara, Sade Sati  │
  │   │                                              │
  │   │  NO DATABASE. NO CLOCK. NO LLM. NO NETWORK.  │
  │   └─────────────────────────────────────────────┘
  │           │  Chart JSON (schema-versioned)
  │           ▼
  │  persist charts + dashas rows
  │  cache in Redis
  ▼
Client
```

**`app/core/` is a pure function library.** No database, no HTTP, no ambient clock —
"now" is passed in, never read. That purity is what makes golden-file testing possible
and the whole system auditable. Enforce it: `app/core/` may not import `httpx`,
`asyncpg`, `datetime.now`, or anything from `app/api/`.

The FastAPI layer in `app/api/` is a thin adapter: parse the Pydantic request, call
`core`, return the Pydantic response. All logic lives in `core`.

---

## 3. Schema (`api-service` owns this)

```sql
CREATE TABLE birth_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    label           TEXT NOT NULL DEFAULT 'self',   -- self | partner | child | friend
    birth_date      DATE NOT NULL,                  -- local calendar date
    birth_time      TIME,                           -- local; NULL ⇒ unknown
    time_accuracy   TEXT NOT NULL DEFAULT 'exact',  -- exact | approximate | unknown
    birth_place     TEXT NOT NULL,
    latitude        DOUBLE PRECISION NOT NULL,
    longitude       DOUBLE PRECISION NOT NULL,
    timezone        TEXT NOT NULL,                  -- IANA
    utc_offset_min  INTEGER NOT NULL,               -- resolved historical offset
    utc_instant     TIMESTAMPTZ NOT NULL,           -- source of truth for all maths

    source          TEXT NOT NULL DEFAULT 'user',
    verified        BOOLEAN NOT NULL DEFAULT FALSE,
    version         INTEGER NOT NULL DEFAULT 1,
    superseded_by   UUID REFERENCES birth_profiles(id),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX birth_profiles_user_idx ON birth_profiles (user_id, is_active);

CREATE TABLE charts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    birth_profile_id   UUID NOT NULL REFERENCES birth_profiles(id) ON DELETE CASCADE,
    chart_type         TEXT NOT NULL,               -- D1 | D9 | D10
    calculation_system TEXT NOT NULL DEFAULT 'vedic',
    ayanamsa           TEXT NOT NULL DEFAULT 'lahiri',
    house_system       TEXT NOT NULL DEFAULT 'whole_sign',
    engine_version     TEXT NOT NULL,               -- "pyswisseph-2.10.3+schema1"
    chart_data         JSONB NOT NULL,
    computed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (birth_profile_id, chart_type, calculation_system, ayanamsa, house_system)
);
CREATE INDEX charts_profile_idx ON charts (birth_profile_id);

CREATE TABLE dashas (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chart_id   UUID NOT NULL REFERENCES charts(id) ON DELETE CASCADE,
    system     TEXT NOT NULL DEFAULT 'vimshottari',
    planet     TEXT NOT NULL,
    start_date TIMESTAMPTZ NOT NULL,
    end_date   TIMESTAMPTZ NOT NULL,
    level      SMALLINT NOT NULL,                   -- 1=Maha 2=Antar 3=Pratyantar
    parent_id  UUID REFERENCES dashas(id) ON DELETE CASCADE,
    metadata   JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX dashas_chart_level_idx ON dashas (chart_id, level);
CREATE INDEX dashas_chart_range_idx ON dashas (chart_id, start_date, end_date);

CREATE TABLE transits (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    planet             TEXT NOT NULL,
    sign               TEXT NOT NULL,
    degree             DOUBLE PRECISION NOT NULL,
    is_retrograde      BOOLEAN NOT NULL DEFAULT FALSE,
    timestamp          TIMESTAMPTZ NOT NULL,
    calculation_system TEXT NOT NULL DEFAULT 'vedic',
    ayanamsa           TEXT NOT NULL DEFAULT 'lahiri',
    metadata           JSONB NOT NULL DEFAULT '{}',
    UNIQUE (planet, timestamp, calculation_system, ayanamsa)
);
CREATE INDEX transits_ts_idx ON transits (timestamp DESC);

CREATE TABLE places (
    id           INTEGER PRIMARY KEY,               -- GeoNames ID
    name         TEXT NOT NULL,
    ascii_name   TEXT NOT NULL,
    admin1       TEXT,
    country_code TEXT NOT NULL,
    latitude     DOUBLE PRECISION NOT NULL,
    longitude    DOUBLE PRECISION NOT NULL,
    timezone     TEXT NOT NULL,
    population   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX places_name_trgm ON places USING gin (ascii_name gin_trgm_ops);
CREATE INDEX places_country_pop_idx ON places (country_code, population DESC);
```

### Versioning rules (both matter)

**Birth profiles are never overwritten.** Correcting a birth time creates version 2;
version 1 gets `superseded_by` set and `is_active = false`. Past conversations remain
explicable — you can always answer "which chart was this reading based on?"

**Charts are keyed by their inputs.** The `UNIQUE` constraint on
`(profile, type, system, ayanamsa, house_system)` means changing the ayanamsa produces
a *new* row rather than mutating one. `engine_version` lets you detect and recompute
charts built by an older engine after a library upgrade.

---

## 4. The ephemeris — and a licence decision you must make

### The engine

**Swiss Ephemeris via `pyswisseph`** — the reference implementation for astrological
calculation: sub-arcsecond accuracy, every ayanamsa, every house system, correct nodes,
correct retrogradation. `pyswisseph` is the mature, widely used binding.

```python
# app/core/ephemeris.py
import swisseph as swe

swe.set_sid_mode(swe.SIDM_LAHIRI, 0, 0)

FLAGS = swe.FLG_SWIEPH | swe.FLG_SIDEREAL | swe.FLG_SPEED
# FLG_SPEED is how retrogradation is detected

def julian_day(utc: datetime) -> float:
    hours = utc.hour + utc.minute / 60 + utc.second / 3600
    return swe.julday(utc.year, utc.month, utc.day, hours, swe.GREG_CAL)

def planet_position(jd: float, planet: int) -> PlanetPosition:
    (lon, lat, dist, lon_speed, _, _), _ = swe.calc_ut(jd, planet, FLAGS)
    return PlanetPosition(
        longitude=lon,
        speed=lon_speed,
        is_retrograde=lon_speed < 0,
    )
```

**Data files.** Use `swe.FLG_MOSEPH` (the built-in Moshier analytical ephemeris) to
ship no data files at all — accurate to roughly 0.1 arcsecond for 1800–2200, far
beyond what astrology needs. Use `FLG_SWIEPH` with `.se1` files for pre-1800 births or
maximum precision. Decide once, record it in the ADR, put the flag in config.

### ⚠️ ADR-003 — the licence question, now due

Swiss Ephemeris is **dual-licensed**: AGPL-3.0, or a paid commercial licence from
Astrodienst. `pyswisseph` inherits this.

| Path | Obligation | Fits if |
|---|---|---|
| **AGPL** | Network use triggers source disclosure — you must offer the complete corresponding source to your users | You're open-sourcing, or pre-revenue and comfortable with it |
| **Commercial licence** | One-time fee to Astrodienst | You're shipping closed-source commercial SaaS — which this is |
| **`skyfield` (MIT)** | None | You accept implementing the Vedic layer yourself |

The third option deserves a serious look. `skyfield` is MIT-licensed, pure Python, and
computes accurate geocentric positions from JPL ephemerides. What it does *not* give
you is the Vedic layer — ayanamsa, nakshatras, dashas, vargas, yogas. But **all of that
is deterministic arithmetic on top of longitudes**, and you are writing it yourself
regardless. Lahiri ayanamsa is a published polynomial; nakshatra is
`floor(longitude / 13°20')`; Vimshottari is a fixed 120-year proportional sequence.

**Free and legally clean for development either way.** But close ADR-003 with a real
decision before Phase 7 — the moment money changes hands, "we'll sort the licence out
later" becomes a liability. This is a checklist item in this phase's gate.

---

## 5. Time, place and the things that quietly break charts

These three problems cause the overwhelming majority of wrong charts.

### 5.1 Historical timezone offsets

The ascendant moves roughly one degree every four minutes. A 30-minute offset error
can change the rising sign — and with it every house placement in the chart.

India is a good example of why "just use +05:30" fails:

| Period | Offset |
|---|---|
| Before 1906 | Local mean time, varying by longitude |
| 1906 onward | IST, UTC+05:30 |
| 1942–1945 | Wartime DST, UTC+06:30 for parts of the year |

**Resolved in Go**, because `api-service` owns the profile and stores `utc_instant`:

```go
// Go stdlib carries the full IANA tzdata, including historical transitions.
loc, err := time.LoadLocation(tzName)          // "Asia/Kolkata"
local := time.Date(y, m, d, hh, mm, 0, 0, loc)
utcInstant := local.UTC()
_, offsetSec := local.Zone()
```

Ship the `tzdata` package (`import _ "time/tzdata"`) so the binary is self-contained
and doesn't depend on the container having a zoneinfo database.

Timezone *name* from coordinates uses `timezonefinder` in Python — offline, free — via
a small `astro-service` endpoint, or a pre-populated `places.timezone` column from the
GeoNames import (preferred: no call needed for a selected place).

Never compute an offset by hand. Never store a naive local timestamp as if it were UTC.

### 5.2 Unknown or approximate birth time

A large fraction of Indian users genuinely do not know their birth time. Don't block
them, and don't silently fabricate a chart.

| `time_accuracy` | Behaviour |
|---|---|
| `exact` | Everything computed and shown |
| `approximate` | Everything computed; UI shows a caveat; ascendant and house-dependent readings flagged lower confidence |
| `unknown` | Use 12:00 local **only** for planetary longitudes. Ascendant, houses and Vimshottari dashas are **omitted** — not guessed |

When the time is unknown the Moon can change nakshatra within the day, so Vimshottari
cannot be computed honestly. Return `null` and have the UI ask for the time rather than
inventing an answer. This is the determinism principle applied to missing input: no
fabrication, anywhere.

The Pydantic response model makes this explicit — `ascendant: Ascendant | None`,
`dashas: list[Dasha] | None` — so a caller cannot accidentally treat a missing
ascendant as present.

### 5.3 Place resolution — self-host GeoNames

Free, offline, no rate limit, faster than any API call.

```bash
curl -O https://download.geonames.org/export/dump/cities500.zip
curl -O https://download.geonames.org/export/dump/admin1CodesASCII.txt
```

Import into `places` (~200k rows for `cities500`; `cities15000` is a lighter ~25k).
A Go `cmd/seed-places` command does the import — it belongs with the service that owns
the table.

```sql
-- name: SearchPlaces :many
SELECT * FROM places
WHERE ascii_name ILIKE $1 || '%'
ORDER BY population DESC
LIMIT $2;
```

Ranking by population is what makes "Jaipur" return Jaipur, Rajasthan first rather than
a village of 600 people. Small detail, large effect on the onboarding funnel.

Always let the user override coordinates manually — village births are the common
failure case, and a map picker solves it.

---

## 6. Chart computation reference

### Ayanamsa
Lahiri (Chitrapaksha) is the Indian standard and the default.
`sidereal = tropical − ayanamsa`. Support Raman and KP as config; never hardcode.

### Signs and nakshatras

```python
SIGN_ARC = 30.0
NAKSHATRA_ARC = 360.0 / 27      # 13°20'
PADA_ARC = NAKSHATRA_ARC / 4    # 3°20'

sign_index = int(lon // SIGN_ARC)                        # 0..11
degree_in_sign = lon % SIGN_ARC
nakshatra_index = int(lon // NAKSHATRA_ARC)              # 0..26
pada = int((lon % NAKSHATRA_ARC) // PADA_ARC) + 1        # 1..4
```

### Houses
**Whole sign** is the default for Vedic and the simplest to get right: the ascendant's
sign is house 1 entirely, the next sign is house 2, and so on. Placidus and Sripati are
available via `house_system` for users who want bhava chalit; they are not the default.

### Grahas
Sun, Moon, Mars, Mercury, Jupiter, Venus, Saturn, Rahu, Ketu.
Rahu = north lunar node using the **mean** node (`swe.MEAN_NODE`), which is the Vedic
convention; Ketu = Rahu + 180°.

Per planet record: sidereal longitude, sign, degree in sign, house, nakshatra, pada,
retrograde (`speed < 0`), combustion (within the traditional orb of the Sun), dignity
(exalted / debilitated / own sign / moolatrikona / neutral).

### Aspects (graha drishti)
All planets aspect the 7th house from themselves. Additionally:
Mars → 4th and 8th · Jupiter → 5th and 9th · Saturn → 3rd and 10th.

### Vimshottari Dasha
120-year cycle, seeded by the Moon's nakshatra at birth.

| Lord | Years | | Lord | Years |
|---|---|---|---|---|
| Ketu | 7 | | Rahu | 18 |
| Venus | 20 | | Jupiter | 16 |
| Sun | 6 | | Saturn | 19 |
| Moon | 10 | | Mercury | 17 |
| Mars | 7 | | **Total** | **120** |

The balance of the first Mahadasha at birth is proportional to how far the Moon has
travelled through its nakshatra. Antardashas subdivide each Mahadasha in the same
sequence, proportionally; Pratyantardashas subdivide again. Three levels are computed;
the tree is persisted in `dashas` via `parent_id`.

**Use `decimal.Decimal` for the proportional arithmetic**, not `float`. Accumulating
float error across three levels of subdivision produces dates that drift by days — and
a dasha date that is wrong by a few days looks plausible and is invisible without a
reference.

### Transits (gochara)
Current positions relative to the natal Moon sign (the Vedic convention) and to natal
houses. Computed every 6 hours by a Go `asynq` worker calling `astro-service`, cached in
the `transits` table keyed by `(planet, timestamp, system, ayanamsa)`.

**Sade Sati** — Saturn transiting the 12th, 1st and 2nd signs from the natal Moon — is
the single most-asked-about transit in Indian astrology. Compute it explicitly, with
phase (rising / peak / setting) and precise start and end dates.

### Yogas
Start with a small, correct set rather than a large, shaky one:
Gajakesari · the five Panch Mahapurusha (Ruchaka, Bhadra, Hamsa, Malavya, Sasa) ·
Budhaditya · Kemadruma · Neecha Bhanga · Chandra-Mangal · basic Raja yogas.

Each yoga is a pure predicate over the chart, with a test constructing a chart that
satisfies it and one that narrowly doesn't.

---

## 7. Chart JSON schema

Versioned from day one — this JSON is consumed by the AI layer for the rest of the
project's life. Defined as Pydantic models in `astro-service`, which is what generates
the OpenAPI contract the Go client is built from.

```jsonc
{
  "schema_version": 1,
  "meta": {
    "calculation_system": "vedic",
    "ayanamsa": "lahiri",
    "ayanamsa_value": 24.1523,
    "house_system": "whole_sign",
    "engine_version": "pyswisseph-2.10.3+schema1",
    "computed_at": "2026-09-13T10:00:00Z",
    "time_accuracy": "exact"
  },
  "ascendant": { "sign": "Aries", "sign_index": 0, "degree": 12.42,
                 "nakshatra": "Ashwini", "pada": 4 },
  "planets": [
    {
      "planet": "Sun", "longitude": 135.23, "sign": "Leo", "sign_index": 4,
      "degree": 15.23, "house": 5, "nakshatra": "Purva Phalguni", "pada": 1,
      "is_retrograde": false, "is_combust": false, "dignity": "own_sign",
      "aspects": [11]
    }
  ],
  "houses": [
    { "house": 1, "sign": "Aries", "sign_index": 0, "lord": "Mars",
      "planets": [], "lord_placed_in_house": 10 }
  ],
  "navamsa": { "ascendant": { "sign": "Sagittarius" }, "planets": [] },
  "yogas": [
    { "name": "Gajakesari Yoga", "strength": "strong",
      "involved_planets": ["Jupiter", "Moon"], "involved_houses": [1, 4] }
  ],
  "summary": {
    "sun_sign": "Leo", "moon_sign": "Taurus", "ascendant_sign": "Aries",
    "moon_nakshatra": "Rohini", "moon_nakshatra_pada": 2
  }
}
```

`summary` exists so the AI context builder and every UI card can read the four facts
they need without walking the whole structure.

---

## 8. API surfaces

### `astro-service` (internal only, never public)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/charts/compute` | birth data → full chart JSON |
| `POST` | `/v1/dashas/compute` | moon longitude + birth instant → 3-level tree |
| `POST` | `/v1/transits/compute` | instant (+ optional natal chart) → transits, Sade Sati |
| `POST` | `/v1/yogas/detect` | chart → detected yogas |
| `POST` | `/v1/timezone/resolve` | lat/lon → IANA zone |
| `GET` | `/health` | |

Stateless, idempotent, cacheable. Requires `X-Internal-Token`. Bound to the internal
network — **must not be reachable from the internet.**

### `api-service` (public)

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/v1/places/search?q=jaip&limit=10` | Self-hosted GeoNames, trigram |
| `POST` | `/api/v1/birth-profiles` | Creates profile + triggers computation |
| `GET` | `/api/v1/birth-profiles` | Caller's profiles only |
| `GET` | `/api/v1/birth-profiles/{id}` | Ownership enforced |
| `PATCH` | `/api/v1/birth-profiles/{id}` | Creates a **new version**; never mutates |
| `DELETE` | `/api/v1/birth-profiles/{id}` | Soft delete + cascade |
| `GET` | `/api/v1/charts/{birthProfileId}` | `?type=D1\|D9\|D10` |
| `GET` | `/api/v1/charts/{birthProfileId}/dashas` | `?level=1,2,3&at=RFC3339` |
| `GET` | `/api/v1/charts/{birthProfileId}/dashas/current` | Active Maha/Antar/Pratyantar |
| `GET` | `/api/v1/astrology/transits` | Global; cached |
| `GET` | `/api/v1/astrology/transits/{birthProfileId}` | Natal-relative, incl. Sade Sati |
| `POST` | `/api/v1/charts/{id}/recompute` | Admin only — after an engine upgrade |

**Authorization on every chart route.** A birth chart is among the most sensitive
objects in the system. Ownership is checked in middleware, and an integration test
asserts that user B gets a **404** (not 403 — don't confirm existence) on user A's chart.

### Resilience on the Go → astro call

```go
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
```

Timeout, two retries with jittered backoff on 5xx/timeout only (never on 4xx), and a
circuit breaker. If `astro-service` is down, a **cached** chart must still be served
from Postgres — a user should be able to view their existing Kundli even when the
compute service is unavailable. Only a *new* profile fails, and it fails with a clear
message.

---

## 9. Testing — the most important section in this document

### Golden-file tests (non-negotiable)

`services/astro/tests/golden/` with ~30 synthetic birth profiles and expected output,
committed:

```
tests/golden/
├── 001-delhi-1990-morning/{input.json, expected_d1.json, expected_dasha.json}
├── 002-mumbai-1985-midnight/          ← date-boundary case
├── 003-chennai-1943-wartime-dst/      ← 1942–45 offset
├── 004-london-1995/                   ← non-Indian timezone
├── 005-anchorage-2000/                ← extreme latitude
├── 006-quito-1988/                    ← equator
├── 007-unknown-birth-time/            ← ascendant + dashas must be null
├── 008-leap-day-2000-02-29/
├── 009-pre-1906-india/                ← local mean time era
├── 010-sign-cusp-ascendant/           ← ascendant within 0.5° of a boundary
└── ...
```

Every fixture is **invented**, not a real person's birth data.

```python
@pytest.mark.parametrize("fixture", load_golden_fixtures(), ids=lambda f: f.name)
def test_chart_matches_golden(fixture):
    chart = compute_chart(fixture.input)
    assert chart.model_dump() == fixture.expected_d1      # exact, not approximate
```

These tests are the contract. When they fail after a dependency upgrade you have caught
a real regression — the alternative is shipping wrong charts silently for weeks.
Regenerating a golden file requires an explicit, reviewed commit with a justification.

### Cross-validation

Verify at least five fixtures against an independent reference (a published ephemeris
table, or a second library) before freezing the golden files. **A golden file that
encodes your own bug is worse than no test at all** — it makes the bug permanent.

### Property tests (`hypothesis`)

This is where Python earns its place in this phase — property-based testing of
numerical code is exactly what `hypothesis` is for.

```python
@given(moon_longitude=floats(min_value=0, max_value=360, exclude_max=True),
       birth=datetimes(min_value=datetime(1900,1,1), max_value=datetime(2100,1,1)))
def test_vimshottari_sums_to_120_years(moon_longitude, birth):
    tree = build_vimshottari(moon_longitude, birth)
    total = sum((d.end - d.start for d in tree.mahadashas), timedelta())
    assert abs(total.days - 120 * 365.25) < 1
```

Properties to assert:
- Mahadasha durations sum to 120 years for **any** Moon longitude
- Every Antardasha sequence sums exactly to its parent's span
- Every Pratyantardasha sequence sums exactly to its parent's span
- All 9 planets appear in exactly one house
- House numbers are a permutation of 1..12
- `Ketu.longitude == (Rahu.longitude + 180) % 360`
- `to_sidereal(to_tropical(x)) == x` within tolerance
- Nakshatra index is always 0..26; pada always 1..4

### Timezone tests (Go side)
- 1943 Kolkata birth resolves to UTC+06:30, not +05:30
- 1900 Kolkata birth uses local mean time
- Same instant expressed in two zones produces an identical chart
- 23:45 on 31 December computes against the correct Julian day

### Integration (Go, testcontainers)
- Create profile → astro called → chart persisted → dashas persisted
- Correcting a birth time creates v2; v1 remains readable with `is_active = false`
- Chart cache hit does **not** call `astro-service` (assert with a test double counter)
- **`astro-service` down → cached chart still served**; new profile fails cleanly
- User B receives 404 on user A's chart
- Deleting a user removes profiles, charts and dashas (extends the Phase 1 test)

### Contract
- Generated Go client round-trips against the live Python service in CI
- Schema change without regenerating fails the `git diff --exit-code contracts/` check

### Performance
- Chart computation < 200 ms in `astro-service`
- Full Go round trip (cold, incl. HTTP) < 350 ms
- Place search < 50 ms p95
- Cached chart read < 10 ms

---

## 10. Caching

| Item | Store | TTL | Key |
|---|---|---|---|
| Computed chart | Postgres + Redis | 24 h Redis, permanent PG | `chart:{profile}:{type}:{ayanamsa}:{houseSystem}` |
| Current dasha | Redis | 6 h | `dasha:current:{chartID}` |
| Global transits | Postgres + Redis | 6 h | `transit:{date}:{ayanamsa}` |
| Place search | Redis | 7 d | `place:q:{normalizedQuery}` |

Chart cache keys include **every input that affects the output**. Omitting `ayanamsa`
from the key is the classic bug: switch a user's preference and they get someone else's
cached chart back.

Global transits are shared across all users and contain no personal data — cache them
aggressively. Per-user transit interpretations are personal — never share those keys.

---

## 11. UI features

Data entry only in this phase; visualisation is Phase 3.

### Birth details flow — `/onboarding/birth`

Three steps, one question per screen. This is the highest-drop-off point in the whole
product; every field you add costs conversion.

```
Step 1/3                    Step 2/3                    Step 3/3
┌────────────────────┐      ┌────────────────────┐      ┌────────────────────┐
│ When were you born?│      │ What time?         │      │ Where?             │
│                    │      │                    │      │                    │
│  ┌──┐ ┌────┐ ┌───┐ │      │   ┌──┐ : ┌──┐      │      │ ┌────────────────┐ │
│  │17│ │Aug │ │1994│ │      │   │14│   │35│      │      │ │ Jaip▌          │ │
│  └──┘ └────┘ └───┘ │      │   └──┘   └──┘      │      │ └────────────────┘ │
│                    │      │                    │      │  Jaipur, Rajasthan │
│                    │      │ □ I don't know my  │      │  Jaipur, Odisha    │
│                    │      │   exact birth time │      │                    │
│                    │      │                    │      │  📍 Pick on map    │
│  [ Continue ]      │      │  [ Continue ]      │      │  [ See my Kundli ] │
└────────────────────┘      └────────────────────┘      └────────────────────┘
```

**Time step.** The "I don't know" checkbox is essential — it converts a dead end into a
completed signup. When checked, explain plainly what becomes unavailable rather than
hiding it: *"We'll show your planetary positions. Your rising sign and dasha periods
need an exact time — you can add it any time later."*

**Place step.** Debounced autocomplete (250 ms) against the local dataset, grouped by
country, ranked by population, with a map fallback for villages.

### Other screens

| Screen | Route | Contents |
|---|---|---|
| Computing | `/onboarding/computing` | 2–4 s celestial animation while the chart computes. Streams real progress; does not fake it. |
| Profile manager | `/settings/birth-profiles` | List, add ("partner", "child"), set active, version history |
| Edit birth details | `/settings/birth-profiles/{id}/edit` | Explicit warning that this creates a new version and past readings stay on the old one |

### States
- **Loading** — skeleton chart card, never a spinner on a blank page
- **Error** — "We couldn't compute your chart" with retry; the technical error goes to Sentry, not the user
- **Empty** — "Add your birth details to get started"
- **Unknown-time** — a persistent, non-nagging banner offering to add the time

---

## 12. Environment variables added

### `astro-service`
```bash
EPHEMERIS_PROVIDER=pyswisseph        # pyswisseph | skyfield
EPHEMERIS_FLAG=moseph                # moseph (no data files) | swieph
EPHEMERIS_PATH=./data/ephe
DEFAULT_AYANAMSA=lahiri              # lahiri | raman | kp
DEFAULT_HOUSE_SYSTEM=whole_sign      # whole_sign | placidus | sripati
DEFAULT_NODE_TYPE=mean               # mean | true
```

### `api-service`
```bash
GEONAMES_DATASET=cities500
PLACE_SEARCH_LIMIT=10
TRANSIT_REFRESH_CRON=0 */6 * * *
CHART_CACHE_TTL=24h
ASTRO_TIMEOUT=10s
ASTRO_MAX_RETRIES=2
ASTRO_CIRCUIT_BREAKER_THRESHOLD=5
```

---

## 13. Task list

| # | Service | Task | Done when |
|---|---|---|---|
| 2.1 | Go | Migration for profiles, charts, dashas, transits, places | Applies; `pg_trgm` present |
| 2.2 | Go | `cmd/seed-places` GeoNames importer | ~200k rows; "jaip" returns Jaipur, Rajasthan first |
| 2.3 | Go | Place search with trigram + population ranking | p95 < 50 ms |
| 2.4 | Go | Historical timezone resolution + `time/tzdata` embedded | 1943 Kolkata fixture resolves to +06:30 |
| 2.5 | Py | `app/core` scaffold; purity lint rule | `core` cannot import httpx/asyncpg/datetime.now |
| 2.6 | Py | `pyswisseph` binding, Julian day, sidereal mode | Sun matches a published reference |
| 2.7 | Py | Planets: longitude, sign, degree, nakshatra, pada, retrograde, combustion, dignity | Fixtures 001–005 pass |
| 2.8 | Py | Ascendant + whole-sign houses | Cusp fixture (010) passes |
| 2.9 | Py | Navamsa (D9) | Fixture passes |
| 2.10 | Py | Vimshottari, 3 levels, `Decimal` arithmetic | Hypothesis properties + golden files pass |
| 2.11 | Py | Aspects (graha drishti) | Unit tested per planet |
| 2.12 | Py | Yoga detection (~10 yogas) | Positive and negative test each |
| 2.13 | Py | Transits + Sade Sati | Matches a reference for a known chart |
| 2.14 | Py | FastAPI routers + Pydantic schemas + OpenAPI export | `task contracts` generates a compiling Go client |
| 2.15 | Go | Typed astro client with timeout, retry, circuit breaker | Killing astro still serves cached charts |
| 2.16 | Go | Birth profile module: create, version, activate, delete | Editing creates v2; v1 readable |
| 2.17 | Go | Chart service: compute, persist, cache, recompute | Cache hit does not call astro |
| 2.18 | Go | Transit refresh `asynq` worker | Runs every 6 h; populates `transits` |
| 2.19 | Go | All endpoints + ownership middleware | User B gets 404 on user A's chart |
| 2.20 | Py | 30 golden fixtures, 5 externally cross-validated | Committed and passing |
| 2.21 | Web | Onboarding UI, 3 steps + computing + profile manager | E2E green |
| 2.22 | — | **Close ADR-003 — the ephemeris licence decision** | Status is `accepted`, not `proposed` |

---

## 14. Security checklist

- [ ] Birth data treated as PII: never logged, never in analytics, never in error messages
- [ ] Ownership middleware on every birth-profile and chart route, tested
- [ ] Cross-user access returns 404, not 403 (no existence disclosure)
- [ ] **`astro-service` not reachable from the internet**; `X-Internal-Token` required
- [ ] `astro-service` receives birth data but stores nothing and logs no PII
- [ ] Place search input validated and length-capped (no unbounded `ILIKE`)
- [ ] Parameterized queries only — `sqlc` by construction
- [ ] Chart cache keys namespaced per profile — no cross-user cache bleed
- [ ] Global transit cache contains no personal data
- [ ] Rate limit on place search (cheap to abuse)
- [ ] Deleting a user cascades to profiles, charts, dashas — integration tested
- [ ] Data export includes birth profiles and charts
- [ ] Ephemeris licence decision recorded and complied with

---

## 15. Analytics events

```
birth_profile_started        { user_id }
birth_profile_step_completed { step }
birth_time_unknown_selected  { user_id }
place_search_performed       { result_count }
place_selected_via_map       { user_id }
birth_profile_created        { user_id, time_accuracy }
chart_generated              { user_id, chart_type, duration_ms }
chart_generation_failed      { error_code }
birth_profile_edited         { user_id, new_version }
```

No dates, times, place names or coordinates in any payload. Enums and IDs only.

---

## 16. Risks

| Risk | Mitigation |
|---|---|
| **Wrong charts shipped silently** | Golden files + external cross-validation of 5 fixtures before freezing. Top risk in the whole project. |
| Ephemeris licence blocks commercialisation | ADR-003 closed in this phase, before any money is involved |
| Historical timezone bugs | Dedicated fixtures for 1943 DST, pre-1906 LMT, non-Indian zones; `time/tzdata` embedded |
| Dasha arithmetic drift | `Decimal` not `float`; hypothesis properties at all three levels; golden files |
| Unknown birth time modelled as a fake time | `time_accuracy` is first-class; `unknown` returns `None`, and the Pydantic types make that explicit |
| `astro-service` becomes a single point of failure | Charts are cached in Postgres; a viewing user is unaffected by astro being down |
| Cross-service latency | Charts are computed once and cached forever; the HTTP hop happens on profile creation, not on every view |
| Onboarding drop-off | One question per screen, "I don't know my time" escape hatch, map fallback |

---

## 17. Definition of Done

Global DoD **plus**:

- [x] 30 golden fixtures pass exactly; 5 externally cross-validated
- [x] `app/core` is pure — no I/O, no ambient clock, enforced by lint
- [x] Every chart output carries `schema_version` and `engine_version`
- [x] Unknown birth time degrades honestly (nulls, not guesses)
- [x] Historical timezone offsets correct across all era fixtures
- [x] Chart computation < 200 ms; Go round trip < 350 ms cold; cached read < 10 ms
- [x] Cached charts still served when `astro-service` is down
- [x] ADR-003 closed with a real decision

---

## 18. Phase Gate 🔒 — CLOSED

Evidence: `docs/TESTING-PHASE-2.md`. It records what was run, and names the
twenty-three guards that were deliberately broken to prove they fire.

- [x] A user can enter birth details and the system stores a versioned `birth_profiles` row
- [x] `GET /charts/{id}` returns a complete, schema-valid D1 chart
- [x] D9 computed and stored
- [x] Vimshottari computed to 3 levels; current dasha queryable by date
- [x] Transits refreshed every 6 h by the worker; Sade Sati detection verified
- [x] ≥10 yogas detected with positive and negative tests each
- [x] **All 30 golden fixtures pass; 5 cross-validated against an independent source**
- [x] **Hypothesis property tests green (120-year sum, house permutation, Rahu/Ketu opposition)**
- [x] Timezone fixtures green, including 1943 DST and pre-1906 LMT
- [x] Place search returns correct, population-ranked results in < 50 ms p95
- [x] Editing birth details creates v2 and preserves v1
- [x] Ownership enforced on every route; cross-user access returns 404
- [x] Unknown birth time returns null ascendant, houses and dashas
- [x] **Cached chart served successfully with `astro-service` stopped**
- [x] Generated Go client compiles from the Python OpenAPI; contract diff check passes
- [x] `astro-service` unreachable publicly; internal token enforced
- [x] User deletion cascades through profiles, charts and dashas
- [x] **ADR-003 (ephemeris licence) is `accepted`, not `proposed`**
- [x] `task verify` green
- [x] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
