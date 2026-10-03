"""The pure parts of retrieval: query building and scoring.

Separated from `retriever.py` so the two things most likely to be wrong —
how a question becomes a text query, and how two scores combine — are
testable without a database.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any

# ── Why `plainto_tsquery` is not used ──
#
# PHASE-05 §4's retrieval SQL calls `plainto_tsquery`, and measuring it
# against the real corpus showed the keyword half of the hybrid
# contributing nothing:
#
#   plainto_tsquery('english', 'career authority')  ->  'career' & 'author'
#       0 matching chunks
#   to_tsquery('english', 'career | authority')
#       3 matching chunks
#
# `plainto_tsquery` ANDs its terms. So does `websearch_to_tsquery`. On any
# multi-word question — which is every real question — the conjunction
# fails, the `FULL OUTER JOIN` yields no keyword rows, and the vector half
# still returns results. The hybrid would be silently vector-only, and §4's
# premise that "keyword search nails exact entities" would be false in
# practice.
#
# Nothing errors. The symptom is retrieval that is half as good as it
# claims, with no way to tell from the outside — which is why this is built
# with OR semantics and why the quality set includes a query the keyword
# half has to answer.

# Words that carry no retrieval signal but would each contribute an OR term.
#
# Deliberately short. An aggressive stop list removes the question's actual
# subject when the subject happens to be a common word — "will" is a stop
# word and also a word about intent, and "house" would be catastrophic to
# drop from an astrology corpus. These are the ones that appear in nearly
# every question and discriminate between none of them.
_STOP_WORDS = frozenset(
    [
        "a",
        "about",
        "am",
        "an",
        "and",
        "any",
        "anything",
        "are",
        "as",
        "at",
        "be",
        "been",
        "being",
        "but",
        "by",
        "can",
        "could",
        "did",
        "do",
        "does",
        "doing",
        "for",
        "from",
        "get",
        "getting",
        "had",
        "has",
        "have",
        "having",
        "he",
        "her",
        "him",
        "his",
        "how",
        "i",
        "if",
        "in",
        "into",
        "is",
        "it",
        "its",
        "just",
        "like",
        "me",
        "might",
        "my",
        "of",
        "on",
        "or",
        "our",
        "should",
        "so",
        "some",
        "tell",
        "than",
        "that",
        "the",
        "their",
        "them",
        "then",
        "there",
        "these",
        "they",
        "this",
        "those",
        "to",
        "us",
        "was",
        "we",
        "were",
        "what",
        "when",
        "where",
        "which",
        "who",
        "whom",
        "why",
        "will",
        "with",
        "would",
        "you",
        "your",
    ]
)

# A term shorter than this is noise in a `to_tsquery` disjunction: single
# letters match stems across the whole corpus.
_MIN_TERM_LENGTH = 2

# Everything `to_tsquery` treats as an operator, replaced with a SPACE.
#
# The space is the point, and it is why this is not redundant with the
# `isalnum` filter below. That filter would also remove an operator — but
# `saturn&venus` would survive it as the single bogus term `saturnvenus`,
# while substituting a space first yields `saturn` and `venus`. A mutation
# deleting this line broke no test until `test_an_operator_splits_a_term`
# was written, which is what established that it does real work rather
# than duplicating the filter.
_TSQUERY_OPERATORS = re.compile(r"[&|!():*<>'\"\\]")


def build_tsquery(text: str) -> str:
    """Turn a question into a `to_tsquery` disjunction.

    Returns "" when nothing usable survives, which the caller must treat
    as "skip the keyword half" rather than as a query matching everything.

    ── Why the terms are sanitised rather than escaped ──

    `to_tsquery` parses its argument, so a question containing `&` or `!`
    is a syntax error raised from inside Postgres. Quoting would preserve
    the characters and still break the parse; stripping them is lossless
    for retrieval, because punctuation carries no lexeme.

    This is also the injection surface. The query text is a parameter, so
    SQL injection is not the risk — but tsquery injection is: a crafted
    term could turn the disjunction into something expensive. Allowing only
    alphanumerics and spaces through closes it by construction rather than
    by escaping correctly.
    """
    cleaned = _TSQUERY_OPERATORS.sub(" ", text.lower())

    terms: list[str] = []
    seen: set[str] = set()
    for raw in cleaned.split():
        term = "".join(char for char in raw if char.isalnum())
        if len(term) < _MIN_TERM_LENGTH or term in _STOP_WORDS or term in seen:
            continue
        seen.add(term)
        terms.append(term)

    # OR, not AND. See the note at the top of this module — this single
    # character is the difference between a hybrid retriever and a vector
    # one wearing a hybrid's name.
    return " | ".join(terms)


@dataclass(frozen=True)
class RetrievedChunk:
    """One chunk, with the scores that selected it.

    The component scores are kept rather than just the combined one
    because "Why am I seeing this?" (§7) renders them, and because a
    retrieval regression is diagnosable from them and not from a total.
    """

    chunk_id: str
    document_id: str
    document_title: str
    category: str
    source: str
    authority: int
    content: str
    metadata: dict[str, Any] = field(default_factory=dict)

    vector_score: float = 0.0
    keyword_score: float = 0.0
    combined_score: float = 0.0

    # True when the chunk's metadata matched one of the chart's actual
    # placements and the boost applied. Surfaced so the explanation can
    # say "this is about Saturn in your 10th house" rather than "this
    # ranked highly".
    boosted: bool = False


# ── Why the two halves are rescaled before they are weighted ──
#
# Because they are not on the same scale, and measuring the hybrid against
# the real corpus showed exactly what that costs.
#
#   cosine similarity   0.45 .. 0.85   (nomic-embed-text)
#   ts_rank             0.00 .. 0.06   (a few matching terms)
#
# At §9's weights of 0.6 and 0.4 the keyword half therefore contributes
# about 4% of the combined score. It cannot promote anything, and the
# measurement said so plainly: "found by keyword search alone: **0**"
# across 48 labelled questions. The hybrid was numerically vector-only —
# a different failure from the `plainto_tsquery` one, with the same
# symptom and the same invisibility.
#
# `What is a bhukti?` is the case that makes it concrete. The word is in
# the antardasha document, keyword search finds it, and the keyword
# contribution of ~0.024 is swamped by a 0.39 vector score for
# phonetically-similar documents — Bhadra Yoga, Bhadrapada nakshatra. The
# right chunk was found and could not win.
#
# Two separate problems need two separate fixes, because the scores do two
# separate jobs:
#
#   RANKING needs the halves COMPARABLE  -> scale ts_rank up
#   The FLOOR needs an ABSOLUTE score    -> rescale cosine from its baseline

# The cosine value `nomic-embed-text` assigns to unrelated text.
#
# Measured, not assumed: four off-domain questions against the 593-chunk
# corpus topped out at 0.450, 0.476, 0.488 and 0.494. The model simply
# does not use the bottom half of [0, 1], so a threshold calibrated as
# though it did sits below the noise floor.
#
# **This constant is model-specific.** A different embedding model has a
# different baseline, and task 5.5's dimension-change runbook has to
# re-measure it — which is why it is named here rather than inlined.
VECTOR_BASELINE = 0.45

# `ts_rank`'s practical ceiling for this corpus and query shape.
#
# Postgres documents no range for `ts_rank`; it depends on term frequency
# and document length. Measured here at roughly 0.06 for a question with a
# few matching terms, so this maps the useful band onto [0, 1].
#
# Clamped rather than normalised within the result set, deliberately:
# min-max normalisation would make the top keyword hit 1.0 on every query,
# including a query whose best keyword match is irrelevant.
TSRANK_CEILING = 0.06


def normalise_vector_score(cosine: float) -> float:
    """Rescale cosine so unrelated text maps to 0 rather than to 0.45.

    Without this the floor cannot separate on-domain from off-domain: the
    measured gap was 0.297 to 0.332, about 10%, and a real question — "I
    keep losing money as fast as I make it" — fell below a floor set
    inside it and retrieved nothing at all.

    Rescaled, the same questions separate by nearly 2x.
    """
    if cosine <= VECTOR_BASELINE:
        return 0.0
    return (cosine - VECTOR_BASELINE) / (1.0 - VECTOR_BASELINE)


def normalise_keyword_score(ts_rank: float) -> float:
    """Scale `ts_rank` onto [0, 1] so the weights mean what they say."""
    if ts_rank <= 0:
        return 0.0
    return min(ts_rank / TSRANK_CEILING, 1.0)


def combine_scores(
    *,
    vector_score: float,
    keyword_score: float,
    vector_weight: float,
    keyword_weight: float,
    boost: float = 1.0,
) -> float:
    """The hybrid score for one chunk, from ALREADY-NORMALISED halves.

    The caller normalises. This function does not, because it is also
    used by the tests to assert the weighting arithmetic in isolation,
    and a function that silently rescaled its inputs would make those
    assertions about something else.

    ── Why the boost multiplies the combination rather than one half ──

    A chunk matching the user's real placements should win "regardless of
    embedding similarity" (§4). Multiplying the combined score preserves
    that intent while keeping the ordering within the boosted set
    unchanged — the best-matching Saturn chunk still beats a worse one.

    Boosting only the vector half was the alternative and it is wrong: a
    chunk found by keyword search alone, with a vector score of zero,
    would get no boost at all — and "Saturn in the 10th house" for a
    native with Saturn in the 10th is exactly the chunk keyword search is
    good at finding.
    """
    combined = vector_score * vector_weight + keyword_score * keyword_weight
    return combined * boost


def order_chunks(chunks: list[RetrievedChunk]) -> list[RetrievedChunk]:
    """Highest combined score first, ties broken by chunk id.

    ── Why the tie-break is explicit ──

    Postgres does not guarantee row order for a query without an
    `ORDER BY` covering every output row, and this query's final SELECT
    has none — the ordering happens here, after the metadata boost, which
    Postgres does not know about. In practice the same plan returns the
    same order, which is exactly what makes this dangerous: a parallel
    scan, a plan change after ANALYZE, or a concurrent vacuum can reorder
    rows that score identically.

    An unstable order would make `scripts/measure_retrieval.py` a sample
    rather than a measurement, and a retrieval regression unattributable.

    A function rather than a lambda inside `retrieve` so the rule is
    testable: a mutation deleting the tie-break survived every test,
    because Python's sort is stable and Postgres happened to return a
    consistent order. Feeding equal-scoring chunks in descending id order
    is the only way to make the difference observable.
    """
    return sorted(chunks, key=lambda chunk: (-chunk.combined_score, chunk.chunk_id))


def _is_house(value: Any) -> bool:
    """A usable house number: an int in 1..12.

    `isinstance(value, bool)` is excluded explicitly because `bool` is a
    subclass of `int` in Python, so a house of `True` would pass an
    `isinstance(value, int)` check and become house 1 — a boost applied to
    the wrong house, silently, with nothing to notice.

    The range is checked because `@>` against `{"house": 13}` matches
    nothing: a bad value does not error, it just quietly produces no
    boost, which is indistinguishable from a corpus that has no matching
    chunk.
    """
    return isinstance(value, int) and not isinstance(value, bool) and 1 <= value <= 12


def placement_filters(facts: dict[str, Any]) -> list[dict[str, Any]]:
    """Metadata predicates for a chart's actual placements.

    Each returned dict is one `metadata @> $n` candidate. They are
    disjunctive: a chunk matching ANY of the native's placements is
    boosted, because a career question should favour both the "Saturn in
    the 10th" chunk and the "10th house" chunk.

    Values are lowercased because `@>` containment is an exact match, and
    the corpus README requires lowercase. A chart supplying "Saturn"
    against a corpus storing "saturn" matches nothing, silently — this
    normalisation is the only thing standing between the boost working and
    the boost being dead code that no test would notice.
    """
    filters: list[dict[str, Any]] = []

    for planet, house in (facts.get("planet_houses") or {}).items():
        name = str(planet).lower()

        # The planet alone, unconditionally. A corpus document about
        # Saturn generally is still more relevant to a native with a
        # prominent Saturn than one about Venus — and that is true
        # regardless of whether the house value is usable.
        #
        # The first version `continue`d on a bad house and so dropped
        # this too, discarding a filter that is independently valid. The
        # malformed-facts test caught it.
        filters.append({"planet": name})

        if _is_house(house):
            filters.append({"planet": name, "house": house})

    for house in facts.get("emphasised_houses") or []:
        if _is_house(house):
            filters.append({"house": house})

    for planet, sign in (facts.get("planet_signs") or {}).items():
        if sign:
            filters.append({"planet": str(planet).lower(), "sign": str(sign).lower()})

    if nakshatra := facts.get("moon_nakshatra"):
        filters.append({"nakshatra": str(nakshatra).lower()})

    # Deduplicated while preserving order, so the parameter list is stable
    # across calls with the same facts — which is what makes a retrieval
    # result reproducible and a regression attributable.
    unique: list[dict[str, Any]] = []
    seen: set[str] = set()
    for item in filters:
        key = repr(sorted(item.items()))
        if key in seen:
            continue
        seen.add(key)
        unique.append(item)

    return unique
