# Golden chart fixtures

**The files live in `tests/fixtures/charts/expected/` at the repository root**, not
here — they are shared with the Go service, which asserts the timezone half of the same
profiles. This directory holds only this note.

## What they are

Thirty synthetic profiles in `tests/fixtures/charts/profiles.json`, each with a frozen
`expected_d1.json` (rasi + navamsa) and `expected_dasha.json`. `tests/test_golden.py`
recomputes all of them and requires an exact match.

They are a **regression net**, not evidence of correctness. They came from this engine,
so they can only ever agree with it. What they catch is a change to chart assembly —
houses, dignities, combustion, padas, dasha subdivision — that nobody decided to make.

## Correctness lives next door

`tests/test_external_reference.py` compares the engine against dated astronomical
events that came from elsewhere: two March equinoxes, the 2020 Jupiter–Saturn great
conjunction, the 2017 total solar eclipse, Mesha Sankranti, three published Saturn
sidereal transits, the 2020 Mars retrograde season, and the sidereal month.

`scripts/generate_golden.py` **refuses to write anything** unless that suite passes
first. A golden file that encodes a bug makes the bug permanent — strictly worse than
no test, because it converts a bug into an assertion.

## Regenerating

```bash
uv run python scripts/build_profiles.py    # profiles.json, offsets from tzdata
uv run python scripts/generate_golden.py   # the expected outputs
```

If a golden file changes and the reference suite still passes, the change is in chart
assembly and **a human has to look at the diff and say why**. Regenerating turns a
failing test green without anyone deciding the new answer is right, which is the one
thing this directory exists to prevent.
