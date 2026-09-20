"""The 200-message labelled set, and what CI can honestly prove with it.

PHASE-04 §12 asks for "≥85% accuracy on the 200-message set" and §13 says
that run is "the 200-message set against the local free model". Those are
two different things and only one of them can run in CI, because
`.claude/rules/testing.md` forbids CI from calling a model at all.

So the suite is split along the line of what is actually checkable
offline:

  **Here, on every run** — the keyword pre-pass, which is pure code. Its
  precision must be 100%, because a wrong keyword match never reaches the
  model and is therefore unrecoverable.

  **`scripts/measure_intent_accuracy.py`, on demand** — end-to-end
  accuracy against a local model, which is where the ≥85% number lives.

Reporting one number for both would be the dishonest option: a CI job
printing "87% accuracy" that had never called a model would be measuring
nothing.
"""

from __future__ import annotations

from collections import Counter
from pathlib import Path

import pytest
from pydantic import BaseModel, Field

from app.classification import Intent, classify_by_keywords

DATASET = Path(__file__).parent / "fixtures" / "intents.jsonl"


class LabelledMessage(BaseModel):
    """One row, validated on load.

    A typo in an intent name would otherwise sit in the file making the
    accuracy number quietly wrong — every prediction for that row counts
    as a miss and nothing says why.
    """

    id: str
    message: str
    intent: Intent
    ambiguous: bool = Field(
        default=False,
        description="Two or more keyword rules match this message, so the pre-pass "
        "must defer to the model rather than pick by rule order.",
    )
    why: str = Field(default="", description="Why this label, for the non-obvious rows.")


def load() -> list[LabelledMessage]:
    rows = [
        LabelledMessage.model_validate_json(line)
        for line in DATASET.read_text().splitlines()
        if line.strip()
    ]
    return rows


DATA = load()


# ─── the dataset as an artifact ──────────────────────────────────────


class TestTheDatasetItself:
    def test_it_has_two_hundred_messages(self) -> None:
        assert len(DATA) == 200

    def test_every_intent_appears(self) -> None:
        """All 21, so the set can detect a regression in any of them.

        A set missing an intent gives a high accuracy score and zero
        information about the intent it omits — and the omitted one is
        usually the rare one, which is where classifiers fail.
        """
        present = {row.intent for row in DATA}
        missing = set(Intent) - present

        assert not missing, f"no labelled example of: {sorted(i.value for i in missing)}"

    def test_no_intent_dominates(self) -> None:
        """Accuracy on a skewed set is a measure of the skew.

        If 60% of rows were GENERAL_ASTROLOGY, a classifier that always
        answered GENERAL_ASTROLOGY would score 60% and be useless.
        """
        counts = Counter(row.intent for row in DATA)
        most_common, count = counts.most_common(1)[0]

        assert count <= len(DATA) * 0.20, (
            f"{most_common.value} is {count}/{len(DATA)} of the set; a constant "
            f"classifier would score {count / len(DATA):.0%} without classifying"
        )

    def test_ids_are_unique(self) -> None:
        ids = [row.id for row in DATA]
        assert len(set(ids)) == len(ids)

    def test_it_is_synthetic(self) -> None:
        """`.claude/rules/testing.md`: fixtures are synthetic only.

        Checked structurally rather than by inspection: a real message
        from a real user would routinely carry a date of birth, a time
        and a place, which in combination is close to a unique
        identifier. None of these rows contains one, because none came
        from a person.
        """
        for row in DATA:
            assert "@" not in row.message, f"{row.id} contains an email address"
            # A birth time like "14:35" or a full DOB. Neither belongs in
            # a committed fixture.
            assert not any(
                token.count(":") == 1 and token.replace(":", "").isdigit()
                for token in row.message.split()
            ), f"{row.id} contains something shaped like a birth time"


# ─── the pre-pass, which is the part CI can measure ──────────────────


def _predictions() -> list[tuple[LabelledMessage, Intent]]:
    out = []
    for row in DATA:
        hit = classify_by_keywords(row.message)
        if hit is not None:
            out.append((row, hit.primary))
    return out


class TestKeywordPrePass:
    def test_precision_is_total(self) -> None:
        """Every message the pre-pass decides, it decides correctly.

        100% and not 95%, because the two failure modes are not
        comparable. A deferred message costs one `fast`-tier call. A
        wrongly decided message never reaches the model at all — it
        retrieves the wrong chart facts, answers in the wrong persona,
        and if the true intent was MEDICAL it skips the posture that
        intent carries. There is no recovery downstream because nothing
        downstream knows a decision was made.
        """
        wrong = [
            (row.id, row.message, row.intent.value, predicted.value)
            for row, predicted in _predictions()
            if predicted is not row.intent
        ]

        assert not wrong, "\n".join(
            f"  {i}  {m!r}  labelled {t}, pre-pass said {p}" for i, m, t, p in wrong
        )

    def test_coverage_is_roughly_the_forty_percent_the_spec_expects(self) -> None:
        """A floor and a ceiling, and the ceiling is the interesting one.

        The floor catches rules rotting away until the saving is gone.
        The ceiling catches someone recovering coverage by loosening the
        rules — which is exactly how precision is lost, and it would
        otherwise look like an improvement.
        """
        decided = len(_predictions())
        fraction = decided / len(DATA)

        assert 0.30 <= fraction <= 0.55, (
            f"pre-pass decides {fraction:.0%} of the set. Below 30% the saving has "
            f"eroded; above 55% the rules have been loosened, and loosening is how "
            f"precision goes."
        )

    def test_it_never_decides_a_general_astrology_message(self) -> None:
        """GENERAL_ASTROLOGY is reached by not being sure.

        It is the fallback for a low-confidence model answer, so a
        keyword path INTO it would convert "tell me about my chart" from
        a deferred message into a confident one and defeat the fallback.
        """
        assert not any(predicted is Intent.GENERAL_ASTROLOGY for _, predicted in _predictions())

    def test_it_never_decides_an_emotional_support_message(self) -> None:
        """The messages nearest the crisis path always reach a model.

        A pre-pass that resolved them would skip the model on exactly
        the messages that most deserve one — and the words that would
        match are the same words the crisis classifier needs to see.
        """
        emotional = [row for row in DATA if row.intent is Intent.EMOTIONAL_SUPPORT]
        assert emotional, "the set must contain distressed messages or this proves nothing"

        for row in emotional:
            assert classify_by_keywords(row.message) is None, (
                f"{row.id} {row.message!r} was decided by keywords; a distressed "
                f"message must reach the model"
            )

    @pytest.mark.parametrize(
        "message",
        [
            "which house rules career in vedic astrology",
            "what does the 7th house signify for marriage generally",
            "is mercury retrograde a real thing or superstition",
            "why do people believe in kundli matching at all",
        ],
    )
    def test_a_question_about_the_system_is_deferred(self, message: str) -> None:
        """Every one of these carries a strong keyword and is not personal.

        Answered as a personal question they retrieve the asker's chart
        for a question that was never about them — confidently, and
        about the wrong subject. These four are the exact rows that made
        the first-person gate necessary.
        """
        assert classify_by_keywords(message) is None

    def test_an_ambiguous_message_is_deferred(self) -> None:
        """Two intents matching means the model decides, not rule order.

        Resolving by specificity is the tempting alternative, and when a
        tie-break picks wrong it produces a confident answer rather than
        an error.
        """
        assert classify_by_keywords("will my marriage survive if i relocate abroad") is None

    def test_the_rows_marked_ambiguous_are_deferred(self) -> None:
        """Rows whose `why` says two intents "fire" — a claim about words.

        The dataset distinguishes two kinds of hard row and only one of
        them is this test's business:

          - **`ambiguous: true`** — two or more rules match the same
            sentence. A lexical fact, and the pre-pass must defer,
            because deciding means picking by rule order.
          - **a `why` note alone** — a human could read it either way,
            but only one rule matches. "My mother has not been well" is
            family-or-health to a reader; to the rules it is FAMILY and
            nothing else, and FAMILY is the label.

        The first version of this test conflated the two and failed on
        that exact row. It was right to: they are different claims, and
        asserting the semantic one against a keyword matcher asks it to
        see something it has no access to. The semantic rows are what
        the model is for.

        The second version grepped the prose `why` field for the word
        "fire", and failed again — on a row whose note claimed FAMILY
        and LEGAL both matched when "uncle" was not in FAMILY at all.
        That failure was worth having twice over: it found a real gap in
        the rules (the extended relations are now there) and it showed
        that an assertion keyed on prose tests the prose. Hence an
        explicit boolean, which can be wrong in a way a test can catch.
        """
        flagged = [row for row in DATA if row.ambiguous]
        assert len(flagged) >= 15, "too few rows are marked ambiguous for this to prove anything"

        for row in flagged:
            assert classify_by_keywords(row.message) is None, (
                f"{row.id} {row.message!r} is marked ambiguous but the pre-pass "
                f"decided it anyway, which means it picked by rule order"
            )
