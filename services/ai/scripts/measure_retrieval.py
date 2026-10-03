"""The retrieval quality set. PHASE-05 §11.

    uv run python -m scripts.measure_retrieval

~50 questions with hand-labelled relevant documents, scored for recall@8
and MRR against the real corpus. §11's target is **recall@8 ≥ 0.85**, and
§16 gates on it.

── Why this is a script and not a pytest module ──

It needs a running Postgres with an ingested corpus AND a running
embedding model. `.claude/rules/testing.md` forbids CI from calling a
language model, and an embedding model is a model: CI would either be slow
and flaky or would silently measure nothing. Same reasoning as
`verify_provider.py` and `verify_local_tiers.py`.

What DOES run in CI is `tests/test_retrieval_query.py` — the query builder
and the scoring — and `tests/test_retrieval_integration.py`, which uses a
deterministic local embedder against a real database.

── Why it builds nothing itself ──

The retriever and the embedder both come from the same factories the
service uses. This repo has been bitten twice by measurement scripts that
constructed their own provider and reported a number for one
configuration under a heading naming another.

── What recall@8 means here ──

A question is "recalled" when at least one of its labelled documents
appears in the top 8. Labelled at DOCUMENT level rather than chunk level,
deliberately: a question about Saturn in the 10th is answered by any chunk
of the Saturn or tenth-house documents, and labelling chunks would make
the metric sensitive to chunk boundaries — which the golden files already
pin and which this is not trying to measure.
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass

from app.providers.factory import embedding_from_settings
from app.retrieval import KnowledgeRetriever
from app.retrieval.retriever import RetrievalFilter
from app.settings import settings


@dataclass(frozen=True)
class Labelled:
    """One question and the documents that should answer it."""

    question: str

    # Document titles. Any one of them in the top k counts as a hit.
    relevant: tuple[str, ...]

    # Why this question is in the set. Not decoration: a quality set
    # whose entries have no stated purpose becomes a list nobody can
    # prune or extend with confidence.
    tests: str


# ── The set ──
#
# Grouped by what each group is for. The groups matter more than the
# count: a set of fifty questions that all exercise vector similarity
# would score well while half the system was dead.

QUESTIONS: tuple[Labelled, ...] = (
    # ── Exact entity questions. The ones §4 says keyword search nails
    # and embeddings blur. These are the group that would fail if
    # `plainto_tsquery` had been copied from the spec.
    Labelled(
        "What does Saturn in the tenth house mean?",
        ("Saturn in the Tenth House", "Saturn (Shani)", "The Tenth House (Karma Bhava)"),
        "exact entity, two words, both present in the corpus",
    ),
    Labelled(
        "Tell me about Rohini nakshatra",
        ("Rohini Nakshatra",),
        "a single rare proper noun — keyword search should dominate",
    ),
    Labelled(
        "What is Gajakesari yoga?",
        ("Gajakesari Yoga",),
        "a named yoga; the name is the whole query",
    ),
    Labelled(
        "What is Mangal dosha?",
        ("Mars (Mangal)", "Reading a Marriage Question"),
        "a term the corpus addresses in two places, one of them guidance",
    ),
    Labelled(
        "Explain Sade Sati",
        ("Sade Sati — Saturn's Seven and a Half Years",),
        "a transliterated term with no English stem to fall back on",
    ),
    Labelled(
        "What does Ketu in my chart signify?",
        ("Ketu (South Lunar Node)",),
        "a short proper noun against a corpus full of planet documents",
    ),
    Labelled(
        "Tell me about Ashlesha",
        ("Ashlesha Nakshatra",),
        "one word, one document — the narrowest possible query",
    ),
    Labelled(
        "What is Vipreet Raja yoga?",
        ("Vipreet Raja Yoga",),
        "a multi-word yoga name; conjunctive matching would find it, OR must too",
    ),
    Labelled(
        "What does Purva Phalguni nakshatra mean?",
        ("Purva Phalguni Nakshatra",),
        "two-word nakshatra; must not collide with Uttara Phalguni",
    ),
    Labelled(
        "Explain Neecha Bhanga",
        ("Neecha Bhanga Raja Yoga",),
        "a term whose document is mostly caveats",
    ),
    # ── Natural-language life questions. What users actually type. These
    # are the group vector search is for: the words in the question are
    # not the words in the corpus.
    Labelled(
        "Should I change my job?",
        ("Reading a Career Question", "The Tenth House (Karma Bhava)"),
        "§4's worked example; no corpus term appears in the question",
    ),
    Labelled(
        "I am worried about my career going nowhere",
        ("Reading a Career Question", "The Tenth House (Karma Bhava)"),
        "emotional framing of a career question",
    ),
    Labelled(
        "When will I get married?",
        ("Reading a Marriage Question", "The Seventh House (Kalatra Bhava)"),
        "a timing question the product declines; retrieval must still find it",
    ),
    Labelled(
        "Is my partner right for me?",
        ("Reading Compatibility (Ashtakoota)", "Reading a Marriage Question"),
        "a verdict question; the compatibility document is the one that refuses it",
    ),
    Labelled(
        "How can I earn more money?",
        ("Reading a Finance Question", "The Eleventh House (Labha Bhava)"),
        "income rather than wealth — must find the 11th, not only the 2nd",
    ),
    Labelled(
        "I keep losing money as fast as I make it",
        (
            "Reading a Finance Question",
            "The Second House (Dhana Bhava)",
            "The Eleventh House (Labha Bhava)",
        ),
        "the 2nd/11th distinction stated in the user's own words",
    ),
    Labelled(
        "What should I study?",
        ("Reading an Education Question", "The Fifth House (Putra Bhava)"),
        "education question with no astrological vocabulary",
    ),
    Labelled(
        "Will I ever move abroad?",
        ("Reading a Travel or Relocation Question", "The Twelfth House (Vyaya Bhava)"),
        "relocation; the 12th is the residence house, not the 9th",
    ),
    Labelled(
        "My relationship with my mother is difficult",
        ("Reading a Family Question", "The Fourth House (Bandhu Bhava)"),
        "family question where the significator is the Moon and the house the 4th",
    ),
    Labelled(
        "I have been feeling low for months",
        ("Reading an Emotional Support Question", "The Moon (Chandra)"),
        "emotional support, adjacent to the crisis path",
    ),
    Labelled(
        "What is my chart like overall?",
        ("Reading a General Question",),
        "the broadest possible question",
    ),
    Labelled(
        "Why does this app say I am a different sign?",
        ("Explaining a Kundli", "Ayanamsa — The Sidereal Correction"),
        "the commonest real question, and no astrological term in it",
    ),
    Labelled(
        "How do I know what period I am in right now?",
        ("The Vimshottari Dasha System", "Reading Antardashas (Sub-Periods)"),
        "dasha question phrased without the word dasha",
    ),
    Labelled(
        "What do the planets passing overhead do to me?",
        ("Reading Transits (Gochara)",),
        "transits described colloquially",
    ),
    Labelled(
        "Do I need to wear a gemstone?",
        (
            "Gemstones (Ratna) — The Tradition and the Caution",
            "Remedies — What This Product Will and Will Not Do",
        ),
        "a remedy question; both documents decline to sell",
    ),
    Labelled(
        "Someone told me I should buy a blue sapphire urgently",
        (
            "Gemstones (Ratna) — The Tradition and the Caution",
            "Remedies — What This Product Will and Will Not Do",
        ),
        "the exploitation pattern, reported by a user",
    ),
    Labelled(
        "I have a headache and a pain in my chest, what does my chart say?",
        ("Medical Questions — The Boundary",),
        "a medical question that MUST retrieve the boundary document",
    ),
    Labelled(
        "Will I win my court case?",
        ("Legal Questions — The Boundary",),
        "a legal question that MUST retrieve the boundary document",
    ),
    Labelled(
        "What can you tell me from my palm?",
        ("Tarot, Numerology and Other Systems",),
        "out of scope; the scope document is the answer",
    ),
    Labelled(
        "I want to talk to a real astrologer",
        ("Tarot, Numerology and Other Systems",),
        "a request for a human",
    ),
    # ── Mechanism questions. Terms that exist in the corpus as concepts
    # rather than as proper nouns, where stems overlap heavily.
    Labelled(
        "How do aspects work in Vedic astrology?",
        ("Graha Drishti — How Planets Aspect",),
        "a concept document; 'aspect' appears in many chunks",
    ),
    Labelled(
        "Which planets aspect the tenth house from where they sit?",
        (
            "Saturn's Aspect (3rd, 7th, 10th)",
            "Graha Drishti — How Planets Aspect",
        ),
        "a structural question; Saturn's 10th aspect is the specific answer",
    ),
    Labelled(
        "What is exaltation and debilitation?",
        ("Planetary Strength — Dignity, Direction and Combustion",),
        "terminology spread across every planet document",
    ),
    Labelled(
        "What is a navamsa chart?",
        ("Divisional Charts (Vargas)",),
        "a term that appears in one document and in passing in others",
    ),
    Labelled(
        "Why do I need my exact birth time?",
        (
            "Explaining a Kundli",
            "The Vimshottari Dasha System",
            "Divisional Charts (Vargas)",
        ),
        "a question three documents answer from different angles",
    ),
    Labelled(
        "What does it mean when a planet is combust?",
        (
            "Planetary Strength — Dignity, Direction and Combustion",
            "Conjunction — Two Planets in One House",
        ),
        "one term, two documents",
    ),
    Labelled(
        "What is the difference between the sixth house and the tenth?",
        (
            "The Sixth House (Shatru Bhava)",
            "The Tenth House (Karma Bhava)",
        ),
        "a comparison; both documents must be reachable from one query",
    ),
    Labelled(
        "Which houses are good for malefic planets?",
        (
            "The Third House (Sahaja Bhava)",
            "The Sixth House (Shatru Bhava)",
            "The Eleventh House (Labha Bhava)",
        ),
        "upachaya houses, a concept named in three documents and defined in none",
    ),
    # ── Sign and planet coverage. Breadth across the corpus rather than
    # depth, to catch a retriever that works only for the documents that
    # happen to be long.
    Labelled("What is Capricorn like?", ("Capricorn (Makara)",), "sign by name"),
    Labelled(
        "Tell me about the sign the Moon is exalted in",
        ("Taurus (Vrishabha)", "The Moon (Chandra)"),
        "a sign named by a property rather than by name",
    ),
    Labelled("What does Jupiter signify?", ("Jupiter (Guru)",), "planet by name"),
    Labelled(
        "Which planet rules discipline and delay?",
        ("Saturn (Shani)",),
        "a planet named by its significations only",
    ),
    Labelled(
        "What is the great benefic?",
        ("Jupiter (Guru)",),
        "a planet named by a classical epithet",
    ),
    Labelled("Explain Pisces", ("Pisces (Meena)",), "sign by name, one word"),
    Labelled(
        "What are the lunar nodes?",
        ("Rahu (North Lunar Node)", "Ketu (South Lunar Node)"),
        "a pair of documents that must both be reachable",
    ),
    # ── Dasha coverage.
    Labelled(
        "What happens during a Saturn period?",
        ("The Saturn Mahadasha (19 years)",),
        "the most feared dasha; the document is mostly correction",
    ),
    Labelled(
        "How long is the Venus dasha?",
        ("The Venus Mahadasha (20 years)", "The Vimshottari Dasha System"),
        "a factual question answerable from a table",
    ),
    Labelled(
        "What is a bhukti?",
        ("Reading Antardashas (Sub-Periods)",),
        "a synonym for antardasha, used in the document only in passing",
    ),
    # ── Questions the corpus should NOT answer well. Included so the
    # min-score floor is measured rather than assumed: a retriever with
    # no floor returns eight chunks for these too, and the model
    # paraphrases them.
    Labelled(
        "What is the capital of France?",
        (),
        "off-domain; the floor should drop everything",
    ),
    Labelled(
        "How do I reset my password?",
        (),
        "product support, not astrology",
    ),
)


async def main() -> int:
    print("resolved configuration")
    print(f"  EMBEDDING_PROVIDER  {settings.embedding_provider}")
    print(f"  EMBEDDING_MODEL     {settings.embedding_model}")
    print(f"  EMBEDDING_DIM       {settings.embedding_dim}")
    print(f"  RAG_TOP_K           {settings.rag_top_k}")
    print(
        f"  weights             vector {settings.rag_vector_weight} / "
        f"keyword {settings.rag_keyword_weight}"
    )
    print(f"  RAG_MIN_SCORE       {settings.rag_min_score}")
    print()

    retriever = await KnowledgeRetriever.connect(embedder=embedding_from_settings())

    try:
        answerable = [q for q in QUESTIONS if q.relevant]
        off_domain = [q for q in QUESTIONS if not q.relevant]

        hits = 0
        reciprocal_ranks: list[float] = []
        keyword_only_hits = 0
        misses: list[tuple[Labelled, list[str]]] = []

        for question in answerable:
            chunks = await retriever.retrieve(question.question, filters=RetrievalFilter())
            titles = [chunk.document_title for chunk in chunks]

            rank = next(
                (index + 1 for index, title in enumerate(titles) if title in question.relevant),
                None,
            )
            if rank is None:
                misses.append((question, titles))
                reciprocal_ranks.append(0.0)
                continue

            hits += 1
            reciprocal_ranks.append(1.0 / rank)

            # Did the keyword half find it? Recorded because a hybrid
            # whose keyword half contributes nothing still scores well on
            # recall, and that is exactly the failure mode the spec's
            # `plainto_tsquery` would have produced.
            found = chunks[rank - 1]
            if found.keyword_score > 0 and found.vector_score == 0:
                keyword_only_hits += 1

        recall = hits / len(answerable)
        mrr = sum(reciprocal_ranks) / len(answerable)

        print(f"recall@{settings.rag_top_k}  {recall:.3f}   ({hits}/{len(answerable)})")
        print(f"MRR         {mrr:.3f}")
        print(f"found by keyword search alone: {keyword_only_hits}")
        print()

        if misses:
            print(f"{len(misses)} miss(es):")
            for question, titles in misses:
                print(f"\n  Q: {question.question}")
                print(f"     tests: {question.tests}")
                print(f"     want any of: {', '.join(question.relevant)}")
                print(f"     got: {', '.join(titles[:5]) or '(nothing above the floor)'}")
            print()

        # The off-domain half of the measurement. Without it, "recall is
        # high" is compatible with a retriever that returns eight chunks
        # for every input including "what is the capital of France".
        print("off-domain questions (the min-score floor):")
        floor_failures = 0
        for question in off_domain:
            chunks = await retriever.retrieve(question.question, filters=RetrievalFilter())
            verdict = "dropped everything" if not chunks else f"returned {len(chunks)}"
            if chunks:
                floor_failures += 1
            print(f"  {verdict:24} {question.question}")
        print()

        target = 0.85
        if recall < target:
            print(f"✗ recall@{settings.rag_top_k} is {recall:.3f}, below §11's {target}")
            return 1

        print(f"✓ recall@{settings.rag_top_k} {recall:.3f} meets §11's {target}")
        if floor_failures:
            print(f"  note: {floor_failures} off-domain question(s) still returned chunks.")
            print("  Raise RAG_MIN_SCORE, or accept that the floor is permissive.")
        return 0
    finally:
        await retriever.close()


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
