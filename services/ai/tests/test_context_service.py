"""AstrologyContextService. PHASE-05 tasks 5.8 and 5.9, tested per §11.

§11 asks for two things from this module:

  "Context builder (Python) — for each of the 21 intents, assert the
   exact set of houses, planets and dasha levels retrieved. Assert
   `fact_index` covers every fact in the context, with nothing extra."

  "Determinism enforcement — the critical one. Feed a chart where Saturn
   is in the 11th, mock a response claiming the 7th, assert the validator
   blocks it. This test is the machine-readable version of Principle 1."

Both are here. The second is the one that matters most in the whole
repository: it is the only place the determinism invariant stops being a
policy and becomes a failing build.
"""

from __future__ import annotations

import json
import re
from datetime import UTC, datetime
from pathlib import Path

import pytest

from app.classification import Intent, IntentResult
from app.context import (
    SELECTIONS,
    AstrologyContextService,
    ChartPayload,
    context_for,
    selection_for,
)
from app.validation import FactIndex, OutputValidator

# A fixed instant, because the dasha depends on it. §3 requires the
# service to be a pure function over the chart JSON, and an ambient clock
# would make both the context and its `context_version` irreproducible.
NOW = datetime(2026, 10, 4, 12, 0, tzinfo=UTC)

FIXTURE_DIR = Path(__file__).resolve().parents[3] / "tests" / "fixtures" / "charts" / "expected"


def _load(profile: str) -> ChartPayload:
    """A real golden chart, from the Phase 2 fixtures.

    Synthetic birth data — `.claude/rules/testing.md` requires that — but
    a REAL computation: these are the cross-validated golden files, so
    the positions are the ones astro-service actually produces. A
    hand-written chart would let a field-name mistake pass, which is the
    commonest way a context builder silently returns nothing.
    """
    chart = json.loads((FIXTURE_DIR / profile / "expected_d1.json").read_text())
    dasha_path = FIXTURE_DIR / profile / "expected_dasha.json"
    dasha = json.loads(dasha_path.read_text()) if dasha_path.exists() else None
    return ChartPayload.from_astro_payload(chart, dasha_tree=dasha)


@pytest.fixture
def chart() -> ChartPayload:
    return _load("008-chennai-2000-leap-day")


def _intent(primary: Intent) -> IntentResult:
    return IntentResult(primary=primary, confidence=0.9)


# ── The chart parses at all ──


def test_a_real_golden_chart_parses(chart: ChartPayload) -> None:
    """The test that catches a field-name mistake.

    A context builder reading `name` where astro-service writes `planet`
    returns an empty chart, which produces an empty fact index, which the
    validator treats as "no chart was supplied" — and the whole failure
    presents as a validator bug.
    """
    assert chart.ascendant.sign == "Libra"
    assert len(chart.planets) == 9
    assert len(chart.houses) == 12

    saturn = chart.planet("Saturn")
    assert saturn is not None
    assert saturn.house >= 1
    assert saturn.sign

    assert chart.lord_of(1) == "Venus"
    assert chart.dasha_periods, "the dasha tree did not load"


def test_the_payload_accepts_both_wrapped_and_unwrapped_chart_data(
    chart: ChartPayload,
) -> None:
    """astro-service returns `{"rasi": {...}, "navamsa": {...}}` for a
    full computation; `charts.chart_data` holds one divisional chart per
    row. Both shapes reach this service, and guessing wrong produces an
    empty chart rather than an error."""
    raw = json.loads((FIXTURE_DIR / "008-chennai-2000-leap-day" / "expected_d1.json").read_text())

    wrapped = ChartPayload.from_astro_payload(raw)
    unwrapped = ChartPayload.from_astro_payload(raw["rasi"])

    assert wrapped.ascendant.sign == unwrapped.ascendant.sign == "Libra"
    assert len(wrapped.planets) == len(unwrapped.planets) == 9


# ── §11: every intent, and the exact selection ──


def test_every_intent_has_a_selection() -> None:
    """All 21, by §10 task 5.8 — "for all 21 intents".

    `selection_for` falls back to the empty selection for an unmapped
    intent, which is the safe direction but is a fallback that should
    never fire in a shipped build. This makes a new unmapped intent a
    test failure instead.
    """
    missing = sorted(i for i in Intent if i not in SELECTIONS)
    assert missing == [], f"these intents have no selection: {missing}"


def test_the_career_selection_is_exactly_what_the_spec_documents() -> None:
    """§3's table row, asserted rather than approximated.

    | CAREER | 10, 6, 2, 11 | 10th lord placement, Saturn, Sun, Mercury,
    current dasha, ... |
    """
    selection = selection_for(Intent.CAREER)

    assert selection.houses == (10, 6, 2, 11), "house order follows §3's table"
    assert selection.house_lords == (10,)
    assert set(selection.planets) == {"saturn", "sun", "mercury"}
    assert selection.include_dasha is True
    assert selection.from_spec is True


@pytest.mark.parametrize(
    ("intent", "houses"),
    [
        (Intent.CAREER, (10, 6, 2, 11)),
        (Intent.MARRIAGE, (7, 2, 4, 8)),
        (Intent.RELATIONSHIP, (5, 7, 11)),
        (Intent.FINANCE, (2, 11, 5, 9)),
        (Intent.EDUCATION, (4, 5, 9)),
        (Intent.FAMILY, (2, 4, 3, 9)),
        (Intent.TRAVEL, (3, 9, 12)),
        (Intent.EMOTIONAL_SUPPORT, (4,)),
        (Intent.GENERAL_ASTROLOGY, (1, 10, 7)),
        (Intent.DASHA, ()),
        (Intent.TRANSIT, ()),
    ],
)
def test_the_spec_table_rows_match_the_spec(intent: Intent, houses: tuple[int, ...]) -> None:
    """Every row §3 states, checked against §3.

    These eleven are the ones the spec defines; the other ten are derived
    and carry a `notes` field saying so. A derived row quietly claiming
    `from_spec=True` would make this suite assert the spec says something
    it does not.
    """
    selection = selection_for(intent)
    assert selection.houses == houses
    assert selection.from_spec is True, f"{intent} is marked as derived but is in §3"


def test_derived_selections_say_they_are_derived() -> None:
    """The ten intents §3's table does not cover.

    Each must be marked `from_spec=False` and must explain itself. A
    selection nobody can justify is a selection nobody can correct.
    """
    in_spec = {
        Intent.CAREER,
        Intent.MARRIAGE,
        Intent.RELATIONSHIP,
        Intent.FINANCE,
        Intent.EDUCATION,
        Intent.FAMILY,
        Intent.TRAVEL,
        Intent.RELOCATION,
        Intent.DASHA,
        Intent.TRANSIT,
        Intent.EMOTIONAL_SUPPORT,
        Intent.GENERAL_ASTROLOGY,
    }

    for intent, selection in SELECTIONS.items():
        if intent in in_spec:
            continue
        assert selection.from_spec is False, f"{intent} claims to be in §3 and is not"
        assert selection.notes, f"{intent} has no stated reason"


# ── §11: the fact index covers the text, with nothing extra ──


def _facts_stated_in(text: str) -> set[tuple[str, int]]:
    """Every (planet, house) pair the rendered context asserts.

    Parsed back out of the prose rather than taken from the builder's
    internals, because the point is that the TEXT and the INDEX agree —
    and reading both from the same variable would prove nothing.
    """
    pairs: set[tuple[str, int]] = set()
    for line in text.splitlines():
        if match := re.match(r"^(\w+): \w+ [\d.]+°, house (\d+)", line):
            pairs.add((match.group(1).lower(), int(match.group(2))))
        if match := re.match(r"^Lord of house \d+: (\w+), in \w+, house (\d+)", line):
            pairs.add((match.group(1).lower(), int(match.group(2))))
    return pairs


@pytest.mark.parametrize("intent", list(Intent))
def test_every_fact_in_the_text_is_in_the_index(intent: Intent, chart: ChartPayload) -> None:
    """The invariant the whole module is built around.

    §11: "Assert `fact_index` covers every fact in the context, with
    nothing extra."

    A prose summary that drifted from the index would make the validator
    block CORRECT answers — a failure that looks like a validator bug,
    gets "fixed" by relaxing the validator, and quietly ends the
    determinism guarantee. Run for all 21 intents, because the drift
    would appear in whichever selection nobody tested.
    """
    text, facts = context_for(chart, intent, now=NOW)

    for planet, house in _facts_stated_in(text):
        assert facts.knows(planet), (
            f"{intent}: the context states {planet} in house {house} and the fact "
            f"index does not know {planet}. The validator would block a response "
            f"that repeated what we told it."
        )
        assert facts.house_of(planet) == house, (
            f"{intent}: the context says {planet} is in house {house}, the index "
            f"says {facts.house_of(planet)}"
        )


@pytest.mark.parametrize("intent", list(Intent))
def test_the_index_claims_nothing_the_text_does_not(intent: Intent, chart: ChartPayload) -> None:
    """The other direction: "with nothing extra".

    An index richer than the text is less dangerous than the reverse —
    it permits claims rather than blocking them — but it still means the
    model may assert something it was never shown, which is guessing that
    happens to be right. The selection decides what the model knows;
    the index must not widen it.
    """
    text, facts = context_for(chart, intent, now=NOW)

    for planet in facts.planets:
        assert planet in text.lower(), (
            f"{intent}: the fact index holds {planet} but the context text never "
            f"mentions it, so a response asserting it would be guessing"
        )


def test_an_empty_selection_produces_no_facts_at_all(chart: ChartPayload) -> None:
    """MEDICAL, LEGAL, TAROT, NUMEROLOGY, HUMAN_ASTROLOGER, OTHER.

    This is a SAFETY property, not a saving. With an empty index the
    validator's strict branch fires on any personal claim, so the model
    cannot build a health or legal interpretation out of chart facts even
    if the safety layer were bypassed.
    """
    for intent in (
        Intent.MEDICAL,
        Intent.LEGAL,
        Intent.TAROT,
        Intent.NUMEROLOGY,
        Intent.HUMAN_ASTROLOGER,
        Intent.OTHER,
    ):
        text, facts = context_for(chart, intent, now=NOW)
        assert text == "", f"{intent} produced chart context: {text!r}"
        assert facts.is_empty, f"{intent} produced facts: {facts}"


# ── §3's worked example ──


def test_the_career_context_produces_the_documented_shape(chart: ChartPayload) -> None:
    """§10 task 5.8: "Career example produces exactly the documented facts".

    §3's example is written against a chart whose 10th is Capricorn with
    Saturn as its lord, which is not this fixture — so what is asserted
    is the SHAPE the example documents: the 10th house with its sign and
    lord, the 10th lord's own placement, the three named planets, and the
    current dasha. The specific values come from the fixture.
    """
    text, facts = context_for(chart, Intent.CAREER, now=NOW)

    assert "Ascendant:" in text
    for house in (10, 6, 2, 11):
        assert f"House {house} (" in text, f"house {house} missing from a career context"

    assert "Lord of house 10:" in text, "the 10th lord's placement is the example's lead"

    for planet in ("Saturn", "Sun", "Mercury"):
        assert planet in text, f"{planet} missing from a career context"

    assert "Mahadasha:" in text
    assert facts.current_dasha, "the fact index did not record the mahadasha"

    # Not a nakshatra question. A career context that dragged in the Moon
    # nakshatra would be the context-bloat §3 is arguing against.
    assert "Moon nakshatra:" not in text


def test_the_career_context_is_far_smaller_than_the_whole_chart(
    chart: ChartPayload,
) -> None:
    """§3: "~700 tokens of precise, relevant, true facts. Versus dumping
    the chart: ~3,000 tokens, most irrelevant."

    KUNDLI is the one intent that legitimately wants everything, so it
    stands in for the dump. The ratio is what the selection buys.
    """
    career, _ = context_for(chart, Intent.CAREER, now=NOW)
    everything, _ = context_for(chart, Intent.KUNDLI, now=NOW)

    assert len(career) < len(everything), "selection is not narrowing anything"
    assert len(career.splitlines()) <= 14, (
        f"a career context is {len(career.splitlines())} lines; §3 budgets ~700 "
        f"tokens and this is meant to be the narrow one"
    )


# ── Dasha ──


def test_the_active_dasha_chain_is_found(chart: ChartPayload) -> None:
    text, facts = context_for(chart, Intent.DASHA, now=NOW)

    assert "Mahadasha:" in text
    assert "Antardasha:" in text
    assert facts.current_dasha
    assert facts.current_antardasha

    # The DASHA intent is the only one that gets the siblings — what is
    # coming next, which is what somebody asking about periods usually
    # wants.
    assert "Antardashas within" in text


def test_only_the_dasha_intent_gets_the_full_tree(chart: ChartPayload) -> None:
    career, _ = context_for(chart, Intent.CAREER, now=NOW)
    assert "Antardashas within" not in career, (
        "a career context carries the whole antardasha list, which is the "
        "context bloat §3 argues against"
    )


def test_dasha_dates_are_rendered_to_the_day_not_the_second(chart: ChartPayload) -> None:
    """The computation is exact — astro-service uses `Decimal` precisely
    so it is — but a response quoting a boundary to the second claims a
    precision the birth time does not support. A time rounded to five
    minutes moves a third-level boundary by hours.
    """
    text, _ = context_for(chart, Intent.DASHA, now=NOW)

    for line in text.splitlines():
        if "Mahadasha:" in line or "Antardasha:" in line:
            assert not re.search(r"\d{2}:\d{2}", line), f"a dasha line quotes a time: {line!r}"
            assert re.search(r"\d{4}-\d{2}-\d{2}", line), f"no date in {line!r}"


def test_a_chart_with_no_dasha_tree_still_builds(chart: ChartPayload) -> None:
    """A chart computed without a birth time has no Moon nakshatra and so
    no Vimshottari sequence. Normal, not an error — treating it as one
    would fail a chat request for a profile that is merely incomplete."""
    timeless = chart.model_copy(update={"dasha_periods": []})

    text, facts = context_for(timeless, Intent.CAREER, now=NOW)

    assert text, "a chart without dashas produced no context at all"
    assert "Mahadasha:" not in text
    assert facts.current_dasha == ""
    assert facts.planets, "the planets should still be there"


# ── No chart ──


@pytest.mark.asyncio
async def test_no_chart_produces_an_empty_context_that_says_so() -> None:
    """The validator treats an empty index strictly, which is correct: a
    personal claim with no chart has invented the chart outright."""
    service = AstrologyContextService(None, now=NOW)

    context = await service.build("user-1", _intent(Intent.CAREER))

    assert context.text == ""
    assert context.facts.is_empty
    assert "no-chart" in context.version, (
        "the version must distinguish 'no chart supplied' from 'this intent "
        "selects nothing' — they are different situations and a log cannot "
        "tell them apart otherwise"
    )


@pytest.mark.asyncio
async def test_the_version_records_the_intent_and_the_ayanamsa(
    chart: ChartPayload,
) -> None:
    """`context_version` is what makes a response from three weeks ago
    explainable. The ayanamsa belongs in it because it changes the
    answer: a chart read under the wrong one moves planets across sign
    boundaries near a cusp and nothing looks wrong."""
    service = AstrologyContextService(chart, now=NOW)

    context = await service.build("user-1", _intent(Intent.CAREER))

    assert "astrology_context.v1" in context.version
    assert "career" in context.version
    assert "lahiri" in context.version


@pytest.mark.asyncio
async def test_the_user_id_is_ignored(chart: ChartPayload) -> None:
    """§12: "`ai-service` cannot fetch arbitrary user charts (it has no
    such query path)."

    The absence of the query is the enforcement. This asserts the
    signature's `user_id` genuinely does nothing — so a prompt injection
    asking for another user's chart has nothing to reach.
    """
    service = AstrologyContextService(chart, now=NOW)

    mine = await service.build("user-1", _intent(Intent.CAREER))
    theirs = await service.build("../../etc/passwd", _intent(Intent.CAREER))

    assert mine.text == theirs.text
    assert mine.facts == theirs.facts


# ── §11's critical test: determinism enforcement ──


def test_a_response_claiming_the_wrong_house_is_blocked(chart: ChartPayload) -> None:
    """§11, verbatim: "Feed a chart where Saturn is in the 11th, mock a
    response claiming the 7th, assert the validator blocks it. This test
    is the machine-readable version of Principle 1."

    This is the most important assertion in the repository. Invariant 1
    says astrology is computed and never generated; topology enforces
    half of it (astro-service has no model access) and this enforces the
    other half — a model cannot CLAIM a position either.
    """
    # Saturn placed in the 11th, deliberately and explicitly, so the test
    # does not depend on where the fixture happens to put it.
    saturn = chart.planet("Saturn")
    assert saturn is not None
    planted = chart.model_copy(
        update={
            "planets": [
                p.model_copy(update={"house": 11}) if p.planet == "Saturn" else p
                for p in chart.planets
            ]
        }
    )

    text, facts = context_for(planted, Intent.CAREER, now=NOW)
    assert facts.house_of("saturn") == 11, "the fixture was not planted correctly"
    assert "house 11" in text

    validator = OutputValidator()

    # The lie. Phrased the way a model actually would.
    violations = validator.validate(
        "Your Saturn is in the 7th house, which is traditionally read as "
        "partnership under constraint.",
        facts,
    )

    assert violations, "a response claiming the wrong house for Saturn was not blocked"
    assert any(v.severity == "block" for v in violations), (
        f"the wrong-house claim was flagged but not blocked: {violations}"
    )


def test_the_same_response_passes_when_it_is_true(chart: ChartPayload) -> None:
    """The control, and it matters as much as the block.

    Without it, "the validator blocks a wrong claim" is also satisfied by
    a validator that blocks everything — which would be a product that
    cannot answer a question, discovered in production.
    """
    planted = chart.model_copy(
        update={
            "planets": [
                p.model_copy(update={"house": 11}) if p.planet == "Saturn" else p
                for p in chart.planets
            ]
        }
    )
    _, facts = context_for(planted, Intent.CAREER, now=NOW)

    violations = OutputValidator().validate(
        "Your Saturn is in the 11th house, which is traditionally read as "
        "income that accumulates slowly.",
        facts,
    )

    blocking = [v for v in violations if v.severity == "block"]
    assert blocking == [], f"a TRUE claim was blocked: {blocking}"


def test_a_personal_claim_with_no_chart_is_blocked() -> None:
    """The empty-index branch, which is the strict one.

    A response making a personal chart claim when no chart was supplied
    has invented the chart outright — the most serious version of this
    failure, not the most forgivable.
    """
    violations = OutputValidator().validate("Your Saturn is in the 10th house.", FactIndex())

    assert any(v.severity == "block" for v in violations), (
        "a personal placement claim with no chart at all was not blocked"
    )


def test_a_general_statement_about_astrology_is_not_blocked() -> None:
    """The distinction the whole fact index rests on.

    "Saturn is traditionally associated with discipline" is a statement
    about astrology. "Saturn is in your 10th house" is a statement about
    the user. Only the second is checkable, and conflating them would
    block every general explanation the product exists to give.
    """
    violations = OutputValidator().validate(
        "Saturn is traditionally associated with discipline and delay, and the "
        "tenth house is read as vocation and public standing.",
        FactIndex(),
    )

    blocking = [v for v in violations if v.severity == "block"]
    assert blocking == [], f"a general explanation was blocked: {blocking}"
