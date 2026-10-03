"""The pure half of retrieval: query building, scoring, placement filters.

No database. The hybrid SQL itself is covered by
tests/test_retrieval_integration.py against real Postgres, because the
parts that matter there — whether `to_tsquery` actually matches, whether
`@>` finds the metadata — are things a fake cannot reproduce.
"""

from __future__ import annotations

import pytest

from app.retrieval.query import (
    RetrievedChunk,
    build_tsquery,
    combine_scores,
    normalise_keyword_score,
    normalise_vector_score,
    order_chunks,
    placement_filters,
)
from app.retrieval.retriever import _contains

# ── build_tsquery ──
#
# The single most consequential function in this module, because getting
# it wrong produces a retriever that works and is quietly half as good.


def test_terms_are_joined_with_or_not_and() -> None:
    """The finding that this module exists to act on.

    PHASE-05 §4's SQL uses `plainto_tsquery`, which ANDs. Measured against
    the real corpus, `plainto_tsquery('english', 'career authority')`
    becomes `'career' & 'author'` and matches **zero** chunks, while the
    OR form matches three.

    On any multi-word question the conjunction fails, the FULL OUTER JOIN
    yields no keyword rows, and the vector half still returns results — so
    the hybrid is silently vector-only and nothing errors.
    """
    query = build_tsquery("career authority")

    assert "|" in query, (
        "terms are ANDed; the keyword half of the hybrid will match nothing "
        "on any multi-word question"
    )
    assert "&" not in query
    assert query == "career | authority"


def test_stop_words_are_dropped() -> None:
    assert build_tsquery("what does my chart say about career") == "chart | say | career"


def test_astrological_terms_survive_the_stop_list() -> None:
    """The stop list is short on purpose.

    "house" would be catastrophic to drop from an astrology corpus, and
    an aggressive list taken from a general-purpose search engine drops
    exactly the words this corpus is about.
    """
    query = build_tsquery("what does the 10th house say about my career")
    for term in ("10th", "house", "career"):
        assert term in query, f"{term!r} was dropped from {query!r}"


def test_tsquery_operators_are_stripped_not_escaped() -> None:
    """`to_tsquery` parses its argument, so an operator is a syntax error.

    This is the injection surface. The text is a bound parameter so SQL
    injection is not the risk — but a crafted term reaching `to_tsquery`
    could turn the disjunction into something expensive, or simply raise
    from inside Postgres on any question containing an ampersand.
    """
    hostile = "saturn & !venus | (jupiter:*) <-> 'moon'"
    query = build_tsquery(hostile)

    for operator in ("&", "!", "(", ")", ":", "*", "<", ">", "'", '"', "\\"):
        if operator == "|":
            continue
        assert operator not in query, f"{operator!r} survived into {query!r}"

    # The actual words are kept. Stripping punctuation must not strip the
    # question.
    for term in ("saturn", "venus", "jupiter", "moon"):
        assert term in query


def test_a_question_of_only_stop_words_yields_an_empty_query() -> None:
    """Empty means "skip the keyword half", not "match everything".

    An empty string passed to `to_tsquery` is a syntax error, so the SQL
    guards on `$2 <> ''`. Without that guard a question like "what is it?"
    raises from inside Postgres.
    """
    assert build_tsquery("what is it") == ""
    assert build_tsquery("") == ""
    assert build_tsquery("   ") == ""
    assert build_tsquery("?!...") == ""


def test_duplicate_terms_appear_once() -> None:
    assert build_tsquery("saturn saturn SATURN") == "saturn"


def test_single_characters_are_dropped() -> None:
    """A one-letter term stems to something that matches the whole corpus."""
    query = build_tsquery("a b saturn")
    assert query == "saturn"


def test_the_query_is_deterministic_and_order_preserving() -> None:
    """Same question, same query — every time, in the question's order.

    Order matters less than determinism here: a retriever whose query
    varies between identical requests cannot be measured, and the
    retrieval quality set is a measurement.
    """
    question = "will saturn in my tenth house affect my career"
    first = build_tsquery(question)
    for _ in range(20):
        assert build_tsquery(question) == first
    assert first.split(" | ") == ["saturn", "tenth", "house", "affect", "career"]


# ── combine_scores ──


def test_scores_combine_by_weight() -> None:
    assert combine_scores(
        vector_score=1.0,
        keyword_score=0.0,
        vector_weight=0.6,
        keyword_weight=0.4,
    ) == pytest.approx(0.6)

    assert combine_scores(
        vector_score=0.5,
        keyword_score=0.5,
        vector_weight=0.6,
        keyword_weight=0.4,
    ) == pytest.approx(0.5)


def test_the_boost_multiplies_the_combination_not_one_half() -> None:
    """A keyword-only hit must still be boostable.

    Boosting only the vector half was the first implementation and it is
    wrong: a chunk found by keyword search alone has a vector score of
    zero, so the boost would be multiplied by nothing. And "Saturn in the
    10th House" for a native with Saturn in the 10th is exactly the chunk
    keyword search is best at finding.
    """
    keyword_only = combine_scores(
        vector_score=0.0,
        keyword_score=0.5,
        vector_weight=0.6,
        keyword_weight=0.4,
        boost=1.35,
    )
    unboosted = combine_scores(
        vector_score=0.0,
        keyword_score=0.5,
        vector_weight=0.6,
        keyword_weight=0.4,
    )

    assert keyword_only > unboosted
    assert keyword_only == pytest.approx(unboosted * 1.35)


def test_the_boost_preserves_ordering_within_the_boosted_set() -> None:
    """A better-matching boosted chunk still beats a worse one.

    The boost is meant to lift relevant chunks above irrelevant ones, not
    to flatten them into a tie.
    """
    better = combine_scores(
        vector_score=0.9,
        keyword_score=0.0,
        vector_weight=0.6,
        keyword_weight=0.4,
        boost=1.35,
    )
    worse = combine_scores(
        vector_score=0.5,
        keyword_score=0.0,
        vector_weight=0.6,
        keyword_weight=0.4,
        boost=1.35,
    )
    assert better > worse


# ── placement_filters ──


def test_placements_become_metadata_predicates() -> None:
    filters = placement_filters({"planet_houses": {"Saturn": 10}, "emphasised_houses": [10, 6]})

    assert {"planet": "saturn", "house": 10} in filters
    # The planet alone, as a weaker match — a document about Saturn
    # generally is still more relevant to this native than one about Venus.
    assert {"planet": "saturn"} in filters
    assert {"house": 10} in filters
    assert {"house": 6} in filters


def test_placement_values_are_lowercased() -> None:
    """The only thing between the boost working and the boost being dead.

    `@>` containment is an exact match, and the corpus README requires
    lowercase metadata. A chart supplying "Saturn" against a corpus
    storing "saturn" matches nothing, silently, and the metadata boost —
    which §4 says "does more for answer quality than any amount of
    embedding-model tuning" — simply never fires.
    """
    filters = placement_filters(
        {
            "planet_houses": {"SATURN": 10},
            "planet_signs": {"Venus": "Libra"},
            "moon_nakshatra": "Rohini",
        }
    )

    for item in filters:
        for key, value in item.items():
            if isinstance(value, str):
                assert value == value.lower(), f"{key}={value!r} is not lowercase"

    assert {"planet": "venus", "sign": "libra"} in filters
    assert {"nakshatra": "rohini"} in filters


def test_filters_are_deduplicated_and_stable() -> None:
    """Same facts, same predicate list, same order.

    Which is what makes a retrieval result reproducible and a regression
    attributable to a change rather than to chance.
    """
    facts = {
        "planet_houses": {"saturn": 10, "sun": 10},
        "emphasised_houses": [10, 10, 6],
    }

    first = placement_filters(facts)
    for _ in range(20):
        assert placement_filters(facts) == first

    assert len(first) == len({repr(sorted(item.items())) for item in first})


def test_malformed_facts_are_ignored_rather_than_raising() -> None:
    """Facts come from the context builder, not from user input — but a
    retrieval that raises on a missing key fails a chat request for a
    chart that is merely incomplete. A chart without a birth time has no
    Moon nakshatra, and that is normal rather than an error.
    """
    assert placement_filters({}) == []
    assert placement_filters({"planet_houses": None}) == []
    assert placement_filters({"emphasised_houses": None}) == []
    assert placement_filters({"moon_nakshatra": None}) == []

    # A non-integer house is dropped rather than passed to Postgres as a
    # string, where `@>` would silently match nothing.
    assert placement_filters({"planet_houses": {"saturn": "tenth"}}) == [{"planet": "saturn"}]


def test_a_house_outside_one_to_twelve_is_rejected() -> None:
    """`@>` against `{"house": 13}` matches nothing.

    A bad value does not error — it quietly produces no boost, which is
    indistinguishable from a corpus with no matching chunk. Rejecting it
    here means a bug in the context builder surfaces as a missing filter
    rather than as retrieval that is mysteriously worse for some charts.
    """
    for bad in (0, 13, -1, 100):
        assert placement_filters({"emphasised_houses": [bad]}) == []

    assert placement_filters({"emphasised_houses": [1, 12]}) == [
        {"house": 1},
        {"house": 12},
    ]


def test_a_boolean_house_is_not_treated_as_house_one() -> None:
    """`bool` is a subclass of `int` in Python.

    So `isinstance(True, int)` is True, and a house of `True` would pass
    a naive check and become house 1 — a boost applied to the wrong
    house, silently.
    """
    assert placement_filters({"emphasised_houses": [True]}) == []
    assert placement_filters({"planet_houses": {"saturn": True}}) == [{"planet": "saturn"}]


# ── Score normalisation ──
#
# The measurement that forced this to exist: across 48 labelled questions
# the hybrid found **0** chunks by keyword search alone, because `ts_rank`
# (~0.06) is an order of magnitude smaller than cosine (~0.7), so at
# weights 0.6/0.4 the keyword half contributed about 4% and could promote
# nothing. After normalising, it rescues one.


def test_unrelated_text_normalises_to_zero() -> None:
    """nomic-embed-text scores unrelated text at 0.45-0.49 cosine.

    Measured against the real corpus: four off-domain questions topped out
    at 0.450, 0.476, 0.488 and 0.494. The model does not use the bottom
    half of [0, 1], so a floor calibrated as though it did sits below the
    noise.
    """
    assert normalise_vector_score(0.45) == 0.0
    assert normalise_vector_score(0.30) == 0.0
    assert normalise_vector_score(0.0) == 0.0


def test_normalisation_widens_the_separation_it_exists_for() -> None:
    """The point of the rescale, in the numbers that motivated it.

    Raw, the best off-domain and the worst on-domain scores were 0.494
    and 0.533 — a 1.08x gap, inside which a real question fell below a
    floor and retrieved nothing. Rescaled, the gap is wide enough to pick
    a threshold with headroom.
    """
    worst_on_domain = normalise_vector_score(0.533)
    best_off_domain = normalise_vector_score(0.494)

    assert best_off_domain < worst_on_domain
    # Raw: 0.533 / 0.494 = 1.08. Rescaled it is better than 1.5x, which
    # is what turns a latent bug into a setting.
    assert worst_on_domain / max(best_off_domain, 1e-9) > 1.5


def test_a_perfect_vector_match_still_normalises_to_one() -> None:
    assert normalise_vector_score(1.0) == pytest.approx(1.0)


def test_ts_rank_is_scaled_onto_a_comparable_range() -> None:
    """Without this the keyword half is numerically irrelevant.

    `ts_rank` has no documented range and measures around 0.06 for a
    question with a few matching terms. Combined raw against a cosine of
    0.7, a 0.4 weight on 0.06 is ~4% of the score — which is why "What is
    a bhukti?" could not be rescued by keyword search even though keyword
    search found it.
    """
    assert normalise_keyword_score(0.0) == 0.0
    assert normalise_keyword_score(0.06) == pytest.approx(1.0)
    assert normalise_keyword_score(0.03) == pytest.approx(0.5)

    # Clamped, not normalised within the result set: min-max normalisation
    # would make the best keyword hit 1.0 on every query, including one
    # whose best keyword hit is irrelevant.
    assert normalise_keyword_score(0.5) == pytest.approx(1.0)
    assert normalise_keyword_score(10.0) == pytest.approx(1.0)


def test_a_strong_keyword_hit_can_now_outrank_a_mediocre_vector_hit() -> None:
    """The behaviour the normalisation buys, stated as an assertion.

    A chunk the keyword half found exactly, with no vector similarity,
    must be able to beat one the vector half found vaguely. Before the
    rescale it could not — which made the hybrid vector-only in
    everything but name.
    """
    keyword_exact = combine_scores(
        vector_score=normalise_vector_score(0.50),
        keyword_score=normalise_keyword_score(0.06),
        vector_weight=0.6,
        keyword_weight=0.4,
    )
    vector_vague = combine_scores(
        vector_score=normalise_vector_score(0.60),
        keyword_score=normalise_keyword_score(0.0),
        vector_weight=0.6,
        keyword_weight=0.4,
    )

    assert keyword_exact > vector_vague, (
        "a chunk found exactly by keyword search cannot outrank a vague "
        "vector match; the keyword half is numerically irrelevant"
    )


def test_an_operator_splits_a_term() -> None:
    """The reason the operator regex is not redundant with `isalnum`.

    Both remove an ampersand. But `isalnum` alone turns `saturn&venus`
    into the single bogus term `saturnvenus`, which matches nothing;
    substituting a space first yields two real terms.

    Written because a mutation deleting the regex broke no test — the
    operator-stripping test used spaced-out operators, which the
    `isalnum` filter handles on its own.
    """
    assert build_tsquery("saturn&venus") == "saturn | venus"
    assert build_tsquery("saturn:venus!mars") == "saturn | venus | mars"
    assert build_tsquery("(saturn|venus)") == "saturn | venus"


def test_contains_matches_a_value_inside_a_json_array() -> None:
    """Postgres `@>` semantics for arrays, reproduced in Python.

    `{"topic": "career"}` contains-matches `{"topic": ["career"]}`. The
    boost is evaluated in Python rather than in the SQL — the predicate
    count varies per request, so putting it in the query would mean a
    different query string every time and no statement caching — so
    `_contains` has to agree with Postgres on this.

    Tested directly because no current filter produces an array match:
    `placement_filters` emits planet, house, sign and nakshatra, all
    scalars in the shipped corpus. The branch guards against a document
    authoring `"planet": ["saturn","mars"]`, which the corpus README does
    not forbid — and a mutation breaking it passed every integration test
    because none of them reached it.
    """
    assert _contains({"topic": ["career", "authority"]}, {"topic": "career"})
    assert not _contains({"topic": ["marriage"]}, {"topic": "career"})

    # The whole list is not a member of itself.
    assert not _contains({"topic": ["career"]}, {"topic": ["career"]})

    # Scalars still work, and every key in the predicate must match.
    assert _contains({"planet": "saturn", "house": 10}, {"planet": "saturn"})
    assert _contains({"planet": "saturn", "house": 10}, {"planet": "saturn", "house": 10})
    assert not _contains({"planet": "saturn", "house": 10}, {"planet": "saturn", "house": 7})
    assert not _contains({"planet": "saturn"}, {"planet": "saturn", "house": 10})

    # An absent key is a miss, not a pass. A mutation turning this into a
    # `continue` made every predicate match everything.
    assert not _contains({}, {"planet": "saturn"})


def test_equal_scores_break_on_chunk_id_not_on_arrival_order() -> None:
    """The tie-break, made observable.

    Postgres does not guarantee row order for this query — the final
    SELECT has no ORDER BY, because the ordering depends on the metadata
    boost, which Postgres does not know about. In practice the same plan
    returns the same order, which is what makes it dangerous: a parallel
    scan or a plan change can reorder equal-scoring rows.

    A mutation deleting the tie-break survived every test, because
    Python's sort is stable and the database happened to be consistent.
    Feeding equal-scoring chunks in DESCENDING id order is the only way
    to tell a stable sort from a sorted one.
    """
    chunks = [
        RetrievedChunk(
            chunk_id=chunk_id,
            document_id="d",
            document_title="t",
            category="c",
            source="s",
            authority=50,
            content="x",
            combined_score=0.5,
        )
        for chunk_id in ("ccc", "bbb", "aaa")
    ]

    ordered = order_chunks(chunks)

    assert [chunk.chunk_id for chunk in ordered] == ["aaa", "bbb", "ccc"], (
        "equal scores kept their arrival order; an unstable database row "
        "order would make retrieval non-reproducible"
    )


def test_higher_scores_still_come_first() -> None:
    """The control. A tie-break that ignored the score would pass the
    test above and break everything else."""
    chunks = [
        RetrievedChunk(
            chunk_id=chunk_id,
            document_id="d",
            document_title="t",
            category="c",
            source="s",
            authority=50,
            content="x",
            combined_score=score,
        )
        for chunk_id, score in (("aaa", 0.1), ("zzz", 0.9), ("mmm", 0.5))
    ]

    ordered = order_chunks(chunks)
    assert [chunk.chunk_id for chunk in ordered] == ["zzz", "mmm", "aaa"]
