"""Layer 3, and the check that makes invariant 1 enforceable.

`.claude/CLAUDE.md`: "Astrology is computed, never generated." Topology
handles half of that — `astro-service` cannot reach a model. This handles
the other half, which is a model *claiming* a computation it never did.

The two halves fail differently. The first fails loudly at import time.
The second fails as a fluent, confident sentence about a planet that is
somewhere else, which is why it needs a test file this size.
"""

from __future__ import annotations

import pytest

from app.validation import FactIndex, OutputValidator, PlanetFact, extract_claims

CHART = FactIndex(
    planets={
        "saturn": PlanetFact(sign="aries", house=4, nakshatra="bharani"),
        "jupiter": PlanetFact(sign="leo", house=8),
        "moon": PlanetFact(sign="scorpio", house=11),
    },
    ascendant="capricorn",
    moon_sign="scorpio",
    current_dasha="venus",
)

V = OutputValidator()


def blocks(text: str, facts: FactIndex = CHART) -> bool:
    return OutputValidator.blocks(V.validate(text, facts))


# ─── the distinction the whole design rests on ───────────────────────


class TestGeneralVersusPersonal:
    @pytest.mark.parametrize(
        "text",
        [
            "Saturn is traditionally associated with discipline and delay.",
            "The 10th house is traditionally read as the house of profession.",
            "In Vedic astrology, Jupiter in Leo is considered a strong placement.",
            "Mars in the 7th house is a classical indicator of a spirited partnership.",
            "A Scorpio moon is traditionally described as intense and private.",
        ],
    )
    def test_a_general_statement_is_never_blocked(self, text: str) -> None:
        """This is the product's main job.

        An extractor that matched every bare placement sentence would
        block "what does the 7th house mean?" — one of the commonest
        questions the app gets — and the validator would have made the
        product unable to explain astrology in order to stop it
        inventing astrology.
        """
        assert not blocks(text)
        assert extract_claims(text) == []

    @pytest.mark.parametrize(
        "text",
        [
            "Saturn is in your 10th house.",
            "Saturn sits in your tenth house.",
            "Your Jupiter is in Capricorn.",
            "Your ascendant is Leo.",
            "You are running Saturn mahadasha.",
        ],
    )
    def test_a_false_personal_claim_is_blocked(self, text: str) -> None:
        # The negative case for the block above: a validator that never
        # extracted anything would pass every test in the previous class
        # and nothing here.
        assert blocks(text)


# ─── fabricated chart facts ──────────────────────────────────────────


class TestFabricatedChartFacts:
    def test_a_true_claim_passes(self) -> None:
        """The most important negative case in this file.

        Without it, `return [Violation(...)]` for every claim would pass
        every other test in this class — and block every correct reading
        the product ever produces.
        """
        assert not blocks("Saturn in your 4th house is traditionally read as a focus on home.")

    def test_the_detail_names_the_real_value(self) -> None:
        """Which is what makes the single retry worth making.

        A model told only "that was wrong" writes something else wrong.
        A model told "Saturn is in the 4th, not the 10th" writes the
        right sentence.
        """
        violations = V.validate("Saturn is in your 10th house.", CHART)

        assert violations[0].detail == "saturn's house is 4, not 10"

    def test_a_sanskrit_name_is_recognised(self) -> None:
        """ "Shani" and "Saturn" are the same planet.

        An index that only knew the English name would treat a correct
        Sanskrit claim as unverifiable — and under the empty-index rule
        that means blocking a TRUE statement, which is the worst
        available outcome: a correct reading refused as a fabrication.
        """
        assert not blocks("Shani in your fourth house suggests a focus on home.")
        assert blocks("Shani in your tenth house suggests a focus on career.")

    def test_a_word_ordinal_is_recognised(self) -> None:
        # Models write "tenth" at least as often as "10th", and an
        # extractor that only handled digits would let half the
        # fabrications through while looking like it worked.
        assert blocks("Saturn occupies your tenth house.")
        assert not blocks("Saturn occupies your fourth house.")

    def test_a_wrong_nakshatra_is_blocked(self) -> None:
        assert blocks("Your Saturn is in Rohini nakshatra.")
        assert not blocks("Your Saturn is in Bharani nakshatra.")

    def test_a_property_the_chart_does_not_carry_is_not_blocked(self) -> None:
        """Jupiter has no nakshatra in this index.

        Not provably wrong, so not blocked. Treating "unknown" as
        "false" would block answers whenever the upstream chart happened
        to omit an optional field — a validation failure caused by a
        gap in the data rather than by anything the model did.
        """
        assert not blocks("Your Jupiter is in Magha nakshatra.")

    def test_a_planet_the_chart_does_not_carry_at_all_is_blocked(self) -> None:
        """Different from the case above, and the difference matters.

        A missing nakshatra is an incomplete record. A claim about Mars
        when the chart names no Mars is a placement that came from
        nowhere.
        """
        assert blocks("Mars is in your 3rd house.")

    def test_every_claim_in_a_long_answer_is_checked(self) -> None:
        """Not just the first.

        A reading names five or six placements. A validator stopping at
        the first hit would clear a response whose opening sentence was
        right and whose conclusion was invented — and the conclusion is
        the part people act on.
        """
        text = (
            "Saturn in your 4th house is traditionally read as a focus on home. "
            "Your Jupiter is in Leo, which classically strengthens conviction. "
            "Your Moon is in Aries, and you are running Saturn mahadasha."
        )

        violations = V.validate(text, CHART)
        details = {v.detail for v in violations}

        assert "moon's sign is scorpio, not aries" in details
        assert "the current dasha is venus, not saturn" in details
        assert len(violations) == 2, "a true claim was flagged, or a false one was missed"


class TestTheEmptyIndex:
    @pytest.mark.parametrize(
        "text",
        [
            "Your ascendant is Leo.",
            "You are running Saturn mahadasha.",
            "Saturn is in your 10th house.",
        ],
    )
    def test_a_personal_claim_with_no_chart_is_blocked(self, text: str) -> None:
        """The most serious version of this failure, not the mildest.

        "Unverifiable, so allow it" is the instinctive reading and it is
        exactly backwards: a response making personal placements when no
        chart was supplied did not get them slightly wrong, it invented
        the chart outright.

        The first two cases are the ones that matter. A house claim
        blocks anyway — the per-claim path already rejects a planet the
        chart does not name — so a version of this test using ONLY the
        house claim passed with the `is_empty` branch deleted, which is
        how the first version of it was written. `ascendant` and `dasha`
        take the "not provably wrong" exit in the per-claim path, so for
        those this branch is the only thing between an invented chart
        and the user.
        """
        assert blocks(text, FactIndex())

    def test_a_general_statement_with_no_chart_is_fine(self) -> None:
        """The negative case, and it is the common path.

        Most GENERAL_ASTROLOGY questions carry no chart at all. Blocking
        every answer to them would break the product's most-used route
        in the name of protecting it.
        """
        assert not blocks("Saturn is traditionally associated with discipline.", FactIndex())

    def test_an_index_with_only_an_ascendant_is_not_empty(self) -> None:
        # A chart reduced to one field is still a chart, and claims
        # against it are checkable rather than fabricated wholesale.
        minimal = FactIndex(ascendant="capricorn")

        assert not minimal.is_empty
        assert blocks("Your ascendant is Leo.", minimal)
        assert not blocks("Your ascendant is Capricorn.", minimal)


# ─── prompt leak ─────────────────────────────────────────────────────


class TestPromptLeak:
    PROMPT = (
        "You are a Vedic astrology companion. Never state an outcome as certain, "
        "and always use traditional framing rather than predictive framing."
    )

    def test_a_quoted_instruction_is_caught(self) -> None:
        validator = OutputValidator(system_prompt=self.PROMPT)

        violations = validator.validate(
            "Sure — my instructions say: never state an outcome as certain, and "
            "always use traditional framing rather than predictive framing."
        )

        assert [v.type for v in violations] == ["prompt_leak"]

    def test_ordinary_astrology_prose_does_not_collide(self) -> None:
        """Eight words, because the prompt and the answer share a vocabulary.

        Both talk about houses, planets and traditional framing, so a
        three-word overlap happens constantly. A shingle short enough to
        catch those would block most correct answers.
        """
        validator = OutputValidator(system_prompt=self.PROMPT)

        # Written to deliberately share a SHORT run with the prompt —
        # "use traditional framing" appears in both. At three words that
        # collides and this answer is blocked; at eight it does not. The
        # first version of this test used prose that happened to share
        # nothing, and so passed at any shingle length.
        answer = (
            "In the Vedic tradition, Saturn is read as a teacher rather than a "
            "punishment. Astrologers use traditional framing here: its transits "
            "are described as periods of consolidation rather than loss."
        )
        assert "use traditional framing" in self.PROMPT
        assert "use traditional framing" in answer

        assert not validator.validate(answer)

    def test_the_excerpt_does_not_carry_the_prompt_back(self) -> None:
        """Otherwise the report leaks what it exists to stop leaking.

        An error or log line quoting the whole system prompt is the same
        disclosure by a different route, and a log is the route nobody
        audits.
        """
        validator = OutputValidator(system_prompt=self.PROMPT)

        answer = "never state an outcome as certain and always use"
        violations = validator.validate(answer)

        assert violations
        # The excerpt must be something the model ALREADY wrote. Checking
        # for the whole prompt verbatim — the first version of this test —
        # passes against an implementation that dumps every shingle,
        # because the join is truncated before the full prompt appears
        # contiguously. This is the property that actually matters: the
        # report may not reveal a byte of the prompt the output did not
        # already contain.
        assert violations[0].excerpt in answer.lower()

    def test_no_prompt_means_no_leak_check(self) -> None:
        # Not a silent pass: with no prompt supplied there is nothing to
        # compare against, and inventing a comparison would be worse.
        assert not OutputValidator().validate("never state an outcome as certain")


# ─── guaranteed outcomes and medical claims ──────────────────────────


class TestPhraseRules:
    @pytest.mark.parametrize(
        "text",
        [
            "I guarantee that you will get married before 2028.",
            "You will definitely be promoted this year.",
            "There is no doubt that you will recover financially.",
            "This is 100% certain.",
        ],
    )
    def test_a_guaranteed_outcome_is_blocked(self, text: str) -> None:
        """`.claude/rules/ai.md`: no guaranteed outcomes, ever.

        Marriage, pregnancy, death, medical, legal and financial. The
        product's whole posture is traditional framing — "is
        traditionally read as" — and a guarantee is the one thing that
        makes an astrology app actively harmful rather than merely
        wrong.
        """
        assert blocks(text)

    @pytest.mark.parametrize(
        "text",
        [
            "This combination is traditionally read as a period of steady progress.",
            "Classically, this placement is associated with marriage in the late twenties.",
            "Many astrologers would read this as a favourable window.",
        ],
    )
    def test_traditional_framing_is_not_blocked(self, text: str) -> None:
        # The negative case, and it is most of the product's output. A
        # rule matching "will" or "marriage" would block all three.
        assert not blocks(text)

    @pytest.mark.parametrize(
        "text",
        [
            "Your chart suggests you have diabetes.",
            "You should stop taking your medication during this transit.",
            "This will cure the condition.",
        ],
    )
    def test_a_medical_claim_is_blocked(self, text: str) -> None:
        assert blocks(text)

    def test_pointing_at_a_doctor_is_not_blocked(self) -> None:
        """The posture §7 actually asks for.

        Cultural and spiritual framing, plus a pointer to a qualified
        professional. A rule that blocked the word "doctor" would block
        the correct answer to every health question.
        """
        assert not blocks(
            "This is traditionally seen as a period for rest. For anything to do "
            "with your health, please speak to a doctor."
        )

    def test_predictive_framing_warns_rather_than_blocks(self) -> None:
        """A tone problem, not a safety one.

        Blocking every predictive sentence would regenerate a large
        share of otherwise good answers — money and latency spent on
        phrasing. It is counted so a rising rate is visible.
        """
        violations = V.validate("You will get married soon.", CHART)

        assert [v.type for v in violations] == ["unsupported_certainty"]
        assert not OutputValidator.blocks(violations)


# ─── the corrective instruction ──────────────────────────────────────


class TestCorrectiveInstruction:
    def test_it_names_the_real_value(self) -> None:
        """Not just "you were wrong".

        A model told only that it failed writes something else wrong on
        the retry, and the single retry is spent for nothing.
        """
        violations = V.validate("Saturn is in your 10th house.", CHART)

        instruction = OutputValidator.corrective_instruction(violations)

        assert "saturn's house is 4, not 10" in instruction

    def test_it_ignores_warnings(self) -> None:
        # A warning did not stop the response, so there is nothing to
        # correct — and padding the instruction with things that did not
        # matter dilutes the one that did.
        violations = V.validate("You will get married soon.", CHART)

        assert OutputValidator.corrective_instruction(violations).count("\n") == 0

    def test_it_never_repeats_the_prompt_back(self) -> None:
        """The leak instruction must not quote the leaked text.

        It is the one correction where naming the specific error would
        re-send the thing being protected.
        """
        validator = OutputValidator(system_prompt=TestPromptLeak.PROMPT)
        violations = validator.validate("never state an outcome as certain and always use")

        instruction = OutputValidator.corrective_instruction(violations)

        assert "never state an outcome as certain" not in instruction


def test_blocks_are_reported_before_warnings() -> None:
    """A reviewer reads the top of the list.

    A response with one fabricated placement and four tone warnings
    should not bury the fabrication at position five.
    """
    text = "You will get married soon, and Saturn is in your 10th house."

    violations = V.validate(text, CHART)

    assert violations[0].severity == "block"


# ─── the corpus that replaced the proximity window ───────────────────
#
# The first extractor joined a planet to a possessive with a wildcard
# window, `[^.!?]{0,40}?`, which had no grammatical content and matched
# straight across a clause boundary. Four of the five realistic general
# sentences below were blocked as fabrications — a true answer to a
# common question, refused, regenerated, refused again, and replaced
# with the graceful fallback.
#
# Three tables rather than scattered assertions, because the property is
# a BALANCE. Tightening the patterns until nothing false-positives is
# easy and useless; the middle table is what stops that, and the third
# is what stops the first two being satisfied by extracting nothing.

GENERAL_STATEMENTS = [
    "Saturn rules discipline, and your 10th house is career.",
    "Jupiter is the planet of expansion, and your 9th house rules higher learning.",
    "Your Venus is strong, as it always is in Libra.",
    "- Saturn is the planet of structure\n- Your 10th house concerns profession",
    "Mars governs energy. Your 3rd house covers siblings.",
    "The 10th house is traditionally read as the house of profession.",
    "Saturn is traditionally associated with discipline and delay.",
    "In Vedic astrology, Jupiter in Leo is considered a strong placement.",
]

TRUE_PERSONAL_CLAIMS = [
    "Saturn is in your 4th house.",
    "Saturn, the planet of structure, is in your 4th house.",
    "Saturn occupies your fourth house.",
    "Saturn is currently in your 4th house.",
    "Your Jupiter is in Leo.",
    "Your ascendant is Capricorn.",
    "Your moon sign is Scorpio.",
    "You are running Venus mahadasha.",
    "Your current mahadasha is Venus.",
    "Your Saturn is in Bharani nakshatra.",
    "Your 4th house is occupied by Saturn.",
    "You have Saturn in the 4th house.",
    "Saturn in your chart is in Aries.",
]

FALSE_PERSONAL_CLAIMS = [
    "Saturn is in your 10th house.",
    "Saturn, the planet of structure, is in your 10th house.",
    "Saturn occupies your tenth house.",
    "Saturn is currently in your 10th house.",
    "Your Jupiter is in Capricorn.",
    "Your ascendant is Leo.",
    "Your moon sign is Aries.",
    "Your sun sign is Leo.",
    "You are running Saturn mahadasha.",
    "Your current mahadasha is Saturn.",
    "Your Saturn is in Rohini nakshatra.",
    "Your 10th house is occupied by Saturn.",
    "You have Saturn in the 10th house.",
    "Saturn in your chart is in Capricorn.",
]

FULL_CHART = FactIndex(
    planets={
        "saturn": PlanetFact(sign="aries", house=4, nakshatra="bharani"),
        "jupiter": PlanetFact(sign="leo", house=8),
        "venus": PlanetFact(sign="taurus", house=5),
        "moon": PlanetFact(sign="scorpio", house=11),
    },
    ascendant="capricorn",
    moon_sign="scorpio",
    sun_sign="pisces",
    current_dasha="venus",
)


@pytest.mark.parametrize("text", GENERAL_STATEMENTS)
def test_a_general_statement_is_never_extracted(text: str) -> None:
    """The regression. These are the sentences the window broke on.

    Asserted on `extract_claims` as well as on the block, because a
    validator that extracted the phantom claim and then happened to find
    it true would pass the block assertion while still being wrong — and
    would start blocking the moment the chart changed.
    """
    assert extract_claims(text) == [], "phantom claim extracted from a general statement"
    assert not blocks(text, FULL_CHART)


@pytest.mark.parametrize("text", TRUE_PERSONAL_CLAIMS)
def test_a_true_personal_claim_passes(text: str) -> None:
    """Tightening the patterns until nothing false-positives is easy.

    This table is what makes it not trivially satisfiable: every
    sentence here states a placement the chart agrees with, in a
    phrasing a model actually uses.
    """
    assert not blocks(text, FULL_CHART)


@pytest.mark.parametrize("text", FALSE_PERSONAL_CLAIMS)
def test_a_false_personal_claim_is_blocked(text: str) -> None:
    """And this is what stops the other two being satisfied by an
    extractor that returns nothing at all."""
    assert blocks(text, FULL_CHART)


def test_the_three_tables_cover_the_same_phrasings(text: str = "") -> None:
    """Every false phrasing has a true counterpart and vice versa.

    Without this the tables drift: a phrasing gets added to the
    must-block list, the pattern is tightened to catch it, and nothing
    checks that the same phrasing still passes when it is TRUE.
    """
    assert len(TRUE_PERSONAL_CLAIMS) >= 12
    assert len(FALSE_PERSONAL_CLAIMS) >= 12
    assert abs(len(TRUE_PERSONAL_CLAIMS) - len(FALSE_PERSONAL_CLAIMS)) <= 2


def test_an_impossible_house_number_is_not_a_claim() -> None:
    """ "your 40th house" names no house.

    Extracting it would produce a violation about a house that does not
    exist, and the corrective instruction would tell the model to fix a
    placement nobody can hold.
    """
    assert extract_claims("Saturn is in your 40th house.") == []


def test_the_two_luminary_signs_are_checked() -> None:
    """`moon_sign` and `sun_sign` were in the index and read by nothing.

    The index carried two checkable facts that no pattern could
    contradict, so "what's my moon sign" — among the commonest questions
    the product gets — was unguarded.
    """
    assert blocks("Your moon sign is Aries.", FULL_CHART)
    assert not blocks("Your moon sign is Scorpio.", FULL_CHART)
