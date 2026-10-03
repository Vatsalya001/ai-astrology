"""Hybrid retrieval against real Postgres. PHASE-05 task 5.6.

`tests/test_retrieval_query.py` covers the query builder and the scoring
with no database. This file covers what a fake cannot reproduce:

  - `to_tsquery` actually matching, which is the whole reason this module
    does not use §4's `plainto_tsquery`
  - `metadata @> $1` actually finding the keys the chunker wrote
  - the FULL OUTER JOIN merging two candidate sets rather than
    intersecting them
  - the `astro_ro` role being able to read and unable to write

No model is called. The embedder here is a deterministic local function,
so this runs in CI under the rule that CI never calls a language model —
while the DATABASE is real, because the database is what the file is
about.

`scripts/measure_retrieval.py` is the other half: recall@8 and MRR against
the real corpus and a real embedding model, run on demand.
"""

from __future__ import annotations

import hashlib
import os
from collections.abc import AsyncIterator

import asyncpg
import pytest
import pytest_asyncio

from app.retrieval import KnowledgeRetriever
from app.retrieval.retriever import RetrievalFilter
from app.settings import settings

pytestmark = pytest.mark.asyncio

# The writer role. Retrieval reads as `astro_ro`, but the test has to
# create the schema and insert the fixtures, which `astro_ro` cannot do —
# and that inability is itself asserted below.
WRITER_DSN = os.environ.get(
    "TEST_DATABASE_URL", "postgresql://astro:astro@localhost:5433/astro_dev"
)
READER_DSN = os.environ.get(
    "TEST_DATABASE_URL_RO", "postgresql://astro_ro:astro_ro@localhost:5433/astro_dev"
)

# Its own schema, so the test neither reads nor disturbs the real corpus.
# Running against the dev corpus would make the assertions depend on 123
# authored documents that change whenever somebody writes one.
TEST_SCHEMA = "retrieval_test"


class DeterministicEmbedder:
    """Vectors from a hash of the text.

    Not a language model, and deliberately not a good embedder: what is
    being tested is the SQL, and a real model would make the assertions
    depend on semantic similarity that could legitimately change when the
    model does.

    The one property it needs is that similar inputs are NOT similar
    vectors, so a chunk's rank comes from the fixture design rather than
    from accidental semantics.
    """

    def __init__(self, dimensions: int = 768) -> None:
        self._dimensions = dimensions

    @property
    def id(self) -> str:
        return "deterministic-test-embedder"

    @property
    def tier(self) -> str:
        return "local"

    @property
    def dimensions(self) -> int:
        return self._dimensions

    async def embed(self, texts: list[str]) -> list[list[float]]:
        return [self._vector(text) for text in texts]

    async def health_check(self) -> bool:
        return True

    def _vector(self, text: str) -> list[float]:
        # SIGNED components, spanning [-1, 1].
        #
        # The first version used `byte / 255`, so every component was
        # positive and any two hash vectors had a cosine around 0.75 —
        # which made the min-score floor impossible to exercise here,
        # because nothing could score low. A mutation deleting the floor
        # broke no test.
        #
        # Signed components put unrelated texts near zero cosine, which
        # is how a real embedding space behaves and what the floor is
        # calibrated against.
        digest = hashlib.sha256(text.encode()).digest()
        raw = [(digest[index % len(digest)] / 127.5) - 1.0 for index in range(self._dimensions)]
        # L2-normalised, because `vector_cosine_ops` is only the right
        # operator class for unit vectors and a fixture that ignored that
        # would test a configuration the product does not ship.
        norm = sum(value * value for value in raw) ** 0.5 or 1.0
        return [value / norm for value in raw]


@pytest_asyncio.fixture
async def retriever() -> AsyncIterator[KnowledgeRetriever]:
    """A retriever over a freshly seeded schema, or a skip."""
    try:
        writer = await asyncpg.connect(WRITER_DSN, timeout=5)
    except (OSError, asyncpg.PostgresError) as err:
        pytest.skip(f"no Postgres at {WRITER_DSN.split('@')[-1]}: {type(err).__name__}")

    try:
        await _seed(writer)
    finally:
        await writer.close()

    pool = await asyncpg.create_pool(
        READER_DSN,
        min_size=1,
        max_size=2,
        # `search_path` is how the real SQL — which names bare
        # `knowledge_chunks` — reaches the test tables without the query
        # itself being modified for the test. A test that rewrote the
        # query would be testing its own version of it.
        server_settings={"search_path": f"{TEST_SCHEMA},public"},
    )
    instance = KnowledgeRetriever(
        pool=pool,
        embedder=DeterministicEmbedder(),
        # SMALLER than the fixture corpus, on purpose. At the production
        # default of 20 both halves return all four documents, the
        # candidate sets are identical, and the FULL OUTER JOIN is
        # indistinguishable from a LEFT JOIN — which in production loses
        # every keyword-only hit.
        candidate_limit=2,
    )

    yield instance

    await instance.close()


async def _seed(writer: asyncpg.Connection) -> None:
    """Build the schema and the fixtures.

    The DDL mirrors migrations 000007 and 000008 rather than importing
    them, and that is a real tradeoff: duplicated schema can drift. The
    Go integration tests read the migration files directly and assert
    against them, which is where schema fidelity is checked. Here the
    subject is the QUERY, so the columns it touches are what matter.
    """
    await writer.execute("CREATE EXTENSION IF NOT EXISTS vector")
    await writer.execute(f"DROP SCHEMA IF EXISTS {TEST_SCHEMA} CASCADE")
    await writer.execute(f"CREATE SCHEMA {TEST_SCHEMA}")
    await writer.execute(f"GRANT USAGE ON SCHEMA {TEST_SCHEMA} TO astro_ro")

    await writer.execute(f"""
        CREATE TABLE {TEST_SCHEMA}.knowledge_documents (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            title TEXT NOT NULL,
            category TEXT NOT NULL,
            content TEXT NOT NULL,
            language TEXT NOT NULL DEFAULT 'en',
            source TEXT NOT NULL,
            authority SMALLINT NOT NULL DEFAULT 50,
            astrology_system TEXT NOT NULL DEFAULT 'vedic',
            metadata JSONB NOT NULL DEFAULT '{{}}',
            is_active BOOLEAN NOT NULL DEFAULT TRUE,
            version INTEGER NOT NULL DEFAULT 1,
            source_checksum TEXT NOT NULL DEFAULT ''
        )
    """)
    await writer.execute(f"""
        CREATE TABLE {TEST_SCHEMA}.knowledge_chunks (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            document_id UUID NOT NULL
                REFERENCES {TEST_SCHEMA}.knowledge_documents (id) ON DELETE CASCADE,
            content TEXT NOT NULL,
            chunk_index INTEGER NOT NULL,
            token_count INTEGER NOT NULL,
            embedding vector({settings.embedding_dim}),
            embedding_model TEXT NOT NULL,
            metadata JSONB NOT NULL DEFAULT '{{}}',
            tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED
        )
    """)
    await writer.execute(f"GRANT SELECT ON ALL TABLES IN SCHEMA {TEST_SCHEMA} TO astro_ro")

    embedder = DeterministicEmbedder(settings.embedding_dim)

    for document in _FIXTURES:
        document_id = await writer.fetchval(
            f"""INSERT INTO {TEST_SCHEMA}.knowledge_documents
                (title, category, content, source, authority, metadata, language)
                VALUES ($1, $2, $3, 'editorial', $4, $5, $6) RETURNING id""",
            document["title"],
            document["category"],
            document["content"],
            document["authority"],
            document["metadata"],
            document.get("language", "en"),
        )
        vector = (await embedder.embed([document["content"]]))[0]
        await writer.execute(
            f"""INSERT INTO {TEST_SCHEMA}.knowledge_chunks
                (document_id, content, chunk_index, token_count,
                 embedding, embedding_model, metadata)
                VALUES ($1, $2, 0, 50, $3::vector, 'test', $4)""",
            document_id,
            document["content"],
            "[" + ",".join(repr(value) for value in vector) + "]",
            document["metadata"],
        )


# Fixtures chosen so each assertion below has exactly one right answer.
_FIXTURES: tuple[dict[str, object], ...] = (
    {
        "title": "Saturn in the Tenth House",
        "category": "houses",
        "content": (
            "Saturn in the tenth house places discipline in the house of karma. "
            "Recognition arrives later than expected and lasts longer."
        ),
        "authority": 70,
        "metadata": '{"planet":"saturn","house":10,"topic":["career"]}',
    },
    {
        "title": "Venus in the Seventh House",
        "category": "houses",
        "content": (
            "Venus in the seventh house is traditionally read as harmony in "
            "partnership and an eye for a congenial match."
        ),
        "authority": 60,
        "metadata": '{"planet":"venus","house":7,"topic":["marriage"]}',
    },
    {
        "title": "Rohini Nakshatra",
        "category": "nakshatras",
        "content": (
            "Rohini spans ten degrees of Taurus. Its shakti is the power to "
            "make things grow, and its deity is Brahma."
        ),
        "authority": 60,
        "metadata": '{"nakshatra":"rohini","sign":"taurus"}',
    },
    {
        # Genuinely stored as `hi`, not merely named so. The first version
        # of this fixture was English with a Hindi title, which made the
        # language test pass on the absence of any `hi` row rather than on
        # the filter excluding one — so it would also have passed with the
        # filter deleted.
        "title": "A Hindi Document",
        "category": "houses",
        "language": "hi",
        "content": "A tenth house document in another language, with karma in it.",
        "authority": 60,
        "metadata": '{"house":10}',
    },
)


# ── The query itself ──


async def test_the_keyword_half_finds_an_exact_term(
    retriever: KnowledgeRetriever,
) -> None:
    """The reason this module does not use §4's `plainto_tsquery`.

    The deterministic embedder gives "Rohini" no semantic relationship to
    the Rohini document, so a hit here can ONLY have come from the
    keyword half. That makes this the test that would fail against a
    conjunctive tsquery, and against the un-normalised scoring that made
    the keyword contribution 4% of the total.
    """
    chunks = await retriever.retrieve("Rohini nakshatra shakti")

    titles = [chunk.document_title for chunk in chunks]
    assert "Rohini Nakshatra" in titles, (
        f"the keyword half found nothing; got {titles}. With a hash-based "
        f"embedder there is no vector path to this document."
    )

    found = next(c for c in chunks if c.document_title == "Rohini Nakshatra")
    assert found.keyword_score > 0


async def test_a_question_of_only_stop_words_does_not_raise(
    retriever: KnowledgeRetriever,
) -> None:
    """An empty tsquery is a syntax error inside `to_tsquery`.

    The SQL guards on `$2 <> ''`. Without the guard this raises from
    Postgres rather than degrading to vector-only, and the failure
    surfaces as a 500 on a chat message rather than as a weak answer.
    """
    chunks = await retriever.retrieve("what is it")
    assert isinstance(chunks, list)


async def test_tsquery_operators_in_a_question_do_not_raise(
    retriever: KnowledgeRetriever,
) -> None:
    """The sanitisation, end to end through Postgres.

    A question containing `&`, `!` or `:*` reaches `to_tsquery` and is a
    syntax error there. `build_tsquery` strips them; this asserts that
    the stripping is sufficient against the real parser rather than
    against my reading of its grammar.
    """
    for hostile in (
        "saturn & venus",
        "saturn !venus",
        "saturn:*",
        "(saturn | venus) & !mars",
        "saturn <-> venus",
        "'saturn'",
        "saturn\\venus",
    ):
        chunks = await retriever.retrieve(hostile)
        assert isinstance(chunks, list), f"{hostile!r} raised"


async def test_the_category_filter_narrows_before_ranking(
    retriever: KnowledgeRetriever,
) -> None:
    """§4: "a career question does not compete against nakshatra material
    it can never use"."""
    chunks = await retriever.retrieve(
        "Rohini nakshatra shakti", filters=RetrievalFilter(category="houses")
    )
    assert all(chunk.category == "houses" for chunk in chunks)
    assert "Rohini Nakshatra" not in [c.document_title for c in chunks]


async def test_the_category_filter_applies_to_the_keyword_half_too(
    retriever: KnowledgeRetriever,
) -> None:
    """§4 filters only the vector half, which leaks.

    A keyword hit from an excluded category would pass through the
    FULL OUTER JOIN and into the results, so the filter has to be in the
    shared CTE rather than in one branch. "Rohini" matches only the
    nakshatra document by keyword, so if the filter were vector-only it
    would appear here.
    """
    chunks = await retriever.retrieve("Rohini", filters=RetrievalFilter(category="houses"))
    assert "Rohini Nakshatra" not in [c.document_title for c in chunks]


async def test_the_language_filter_works_in_both_directions(
    retriever: KnowledgeRetriever,
) -> None:
    """Both directions, because one of them is not a test.

    The corpus holds one `hi` document whose content deliberately matches
    the same query as the English ones. Asserting only that a `hi` filter
    returns nothing would pass against a corpus holding no `hi` rows at
    all — which is what the first version of this did — and asserting only
    that an `en` filter returns something would pass with the filter
    deleted.
    """
    english = await retriever.retrieve("tenth house karma", filters=RetrievalFilter(language="en"))
    hindi = await retriever.retrieve("tenth house karma", filters=RetrievalFilter(language="hi"))

    english_titles = [chunk.document_title for chunk in english]
    hindi_titles = [chunk.document_title for chunk in hindi]

    assert "A Hindi Document" not in english_titles, (
        "the language filter is not excluding: a Hindi document reached an "
        "English query. That matters beyond tidiness — `tsv` is generated "
        "with the English configuration, so Hindi content is stemmed as "
        "though it were English."
    )
    assert hindi_titles == ["A Hindi Document"], (
        f"a `hi` query returned {hindi_titles}; the filter is not selecting"
    )


# ── The metadata boost ──


async def test_the_metadata_boost_actually_fires(
    retriever: KnowledgeRetriever,
) -> None:
    """The guard that a dead boost would pass without.

    `_decode_metadata` exists because asyncpg returns jsonb as a STRING
    unless a codec is registered, and a retriever that treated that
    string as a dict would find no keys, never boost, and never error.
    The feature §4 calls the one that "does more for answer quality than
    any amount of embedding-model tuning" would simply be absent.
    """
    facts = {"planet_houses": {"saturn": 10}}

    boosted = await retriever.retrieve(
        "tenth house karma discipline", filters=RetrievalFilter(facts=facts)
    )
    plain = await retriever.retrieve("tenth house karma discipline")

    saturn_boosted = next(
        (c for c in boosted if c.document_title == "Saturn in the Tenth House"), None
    )
    saturn_plain = next((c for c in plain if c.document_title == "Saturn in the Tenth House"), None)

    assert saturn_boosted is not None, "the Saturn document was not retrieved at all"
    assert saturn_plain is not None

    assert saturn_boosted.boosted is True, (
        "metadata @> matched nothing. If asyncpg is returning jsonb as a "
        "string and it is not being decoded, the boost is dead code."
    )
    assert saturn_boosted.combined_score > saturn_plain.combined_score
    assert saturn_boosted.combined_score == pytest.approx(
        saturn_plain.combined_score * settings.rag_metadata_boost
    )


async def test_an_unmatched_placement_does_not_boost(
    retriever: KnowledgeRetriever,
) -> None:
    """The negative case. A boost that fired for everything would be
    indistinguishable from no boost, and would pass the test above."""
    chunks = await retriever.retrieve(
        "Rohini nakshatra shakti",
        filters=RetrievalFilter(facts={"planet_houses": {"saturn": 10}}),
    )

    rohini = next((c for c in chunks if c.document_title == "Rohini Nakshatra"), None)
    assert rohini is not None
    assert rohini.boosted is False, (
        "a Saturn-in-10th placement boosted a nakshatra document; the "
        "containment check is matching too broadly"
    )


async def test_the_stored_metadata_keeps_its_json_types(
    retriever: KnowledgeRetriever,
) -> None:
    """Arrays come back as lists, integers as integers.

    This was named "a topic array is matched by containment" and did not
    test that: the boost it asserted came from `planet` and `house`, both
    scalars, and the `topic` array was never a filter key. A mutation
    breaking the list branch of `_contains` passed it.

    Array containment is now tested directly in
    tests/test_retrieval_query.py, where it can be exercised without
    inventing a filter nothing produces. What is worth asserting here is
    the round trip: `house` must be an int and not the string "10",
    because `@>` would silently not match.
    """
    chunks = await retriever.retrieve(
        "tenth house karma",
        filters=RetrievalFilter(facts={"planet_houses": {"saturn": 10}}),
    )
    saturn = next(c for c in chunks if c.document_title == "Saturn in the Tenth House")

    assert saturn.metadata["topic"] == ["career"]
    assert saturn.metadata["house"] == 10
    assert not isinstance(saturn.metadata["house"], str)
    assert saturn.boosted is True


async def test_an_off_domain_question_is_dropped_by_the_floor(
    retriever: KnowledgeRetriever,
) -> None:
    """The floor, exercised rather than assumed.

    The embedder gives unrelated text a cosine near zero, so a question
    with no keyword overlap and no vector similarity must score below
    `RAG_MIN_SCORE` and return nothing at all.

    Without a floor, this returns `candidate_limit` chunks of astrology
    for a question about bread, and the model paraphrases them. A
    nearest-neighbour query has no concept of "no match".
    """
    chunks = await retriever.retrieve("sourdough bread fermentation hydration")
    assert chunks == [], (
        f"an off-domain question returned {[c.document_title for c in chunks]} "
        f"with scores {[round(c.combined_score, 3) for c in chunks]}; the "
        f"min-score floor is not dropping anything"
    )


# ── The read-only role ──


async def test_retrieval_runs_as_a_role_that_cannot_write(
    retriever: KnowledgeRetriever,
) -> None:
    """ADR-001's single-writer rule, asserted rather than assumed.

    Enforced by Postgres grants, not by this module being careful. A test
    that only proved the read half would not notice the day somebody
    grants INSERT to fix something.

    Takes the `retriever` fixture — which it does not otherwise use —
    purely so the schema exists. Written first against
    `public.knowledge_documents`, which does not exist in CI because the
    service container runs no migrations: the test would then fail with
    `UndefinedTableError` instead of `InsufficientPrivilegeError` and
    prove nothing about grants. Pointing it at tables the fixture creates
    and explicitly grants SELECT on makes the refusal attributable to the
    grant rather than to the table being absent.
    """
    assert retriever is not None

    try:
        reader = await asyncpg.connect(READER_DSN, timeout=5)
    except (OSError, asyncpg.PostgresError) as err:
        pytest.skip(f"no Postgres: {type(err).__name__}")

    try:
        # The read half first, so a privilege error below cannot be
        # explained by the table being unreachable.
        count = await reader.fetchval(f"SELECT count(*) FROM {TEST_SCHEMA}.knowledge_chunks")
        assert count == len(_FIXTURES), "astro_ro cannot read what the writer wrote"

        with pytest.raises(asyncpg.InsufficientPrivilegeError):
            await reader.execute(
                f"INSERT INTO {TEST_SCHEMA}.knowledge_documents "
                f"(title, category, content, source) VALUES ('x','x','x','x')"
            )
        with pytest.raises(asyncpg.InsufficientPrivilegeError):
            await reader.execute(f"DELETE FROM {TEST_SCHEMA}.knowledge_chunks")
        with pytest.raises(asyncpg.InsufficientPrivilegeError):
            await reader.execute(f"UPDATE {TEST_SCHEMA}.knowledge_chunks SET content = 'x'")
    finally:
        await reader.close()


# ── Ordering and the cap ──


async def test_results_are_ordered_by_combined_score(
    retriever: KnowledgeRetriever,
) -> None:
    chunks = await retriever.retrieve("tenth house karma discipline partnership")
    scores = [chunk.combined_score for chunk in chunks]
    assert scores == sorted(scores, reverse=True)


async def test_top_k_caps_the_result(retriever: KnowledgeRetriever) -> None:
    chunks = await retriever.retrieve("house", top_k=1)
    assert len(chunks) <= 1


async def test_ordering_is_stable_for_equal_scores(
    retriever: KnowledgeRetriever,
) -> None:
    """Ties break on chunk id rather than on whatever order Postgres
    returned, so an identical question gives an identical answer.

    Which is what makes the retrieval quality set a measurement rather
    than a sample.
    """
    first = await retriever.retrieve("tenth house karma")
    for _ in range(5):
        again = await retriever.retrieve("tenth house karma")
        assert [c.chunk_id for c in again] == [c.chunk_id for c in first]
