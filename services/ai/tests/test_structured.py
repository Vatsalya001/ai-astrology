"""The shared model-JSON parser, and the asymmetry that made it necessary.

This module exists because the same logic lived in two places and the two
drifted — in the worst direction. `IntentClassifier._parse` was hardened
after llama3.2:3b was observed wrapping its answer in a single backtick.
`SafetyClassifier._parse` kept an older guard that fired only on a triple
fence.

The consequence was not symmetric. An unparseable INTENT falls back to
broad context and the user still gets an answer. An unparseable SAFETY
verdict becomes `none`, the crisis branch never fires, and the message
reaches full astrology generation.

So the tests below run every shape against BOTH classifiers and require
them to agree. Agreement is the property; the individual answers are
secondary.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.classification import IntentClassifier
from app.providers import MockProvider
from app.safety import SafetyCategory, SafetyClassifier
from app.structured import parse_json_object

# Every wrapper a local model has been seen to add, or a trivial variant
# of one. The names are what a failure message will print.
WRAPPERS: dict[str, str] = {
    "bare": "{payload}",
    "fenced with a language tag": "```json\n{payload}\n```",
    "bare fence": "```\n{payload}\n```",
    "single backtick": "`{payload}`",
    "leading prose": "Sure, here is the classification: {payload}",
    "surrounding prose": "Here you go: {payload} Let me know if you need more.",
    "leading whitespace": "\n\n  {payload}\n",
    "fence with no newlines": "```{payload}```",
}


def wrapped(payload: dict[str, object], shape: str) -> str:
    return WRAPPERS[shape].format(payload=json.dumps(payload))


# ─── the parser itself ───────────────────────────────────────────────


class TestParseJsonObject:
    @pytest.mark.parametrize("shape", list(WRAPPERS), ids=list(WRAPPERS))
    def test_every_observed_wrapper_is_unwrapped(self, shape: str) -> None:
        payload = {"category": "crisis", "confidence": 0.95}

        assert parse_json_object(wrapped(payload, shape)) == payload

    def test_prose_around_the_object_does_not_swallow_the_braces(self) -> None:
        """Brace matching, not a regex.

        A greedy `\\{.*\\}` would swallow the trailing sentence into the
        JSON and fail to parse — which reads as "the model failed" when
        it answered correctly and merely chatted around the answer.
        """
        text = 'Certainly! {"category": "medical", "confidence": 0.8} Anything else?'

        assert parse_json_object(text) == {"category": "medical", "confidence": 0.8}

    def test_a_brace_inside_a_string_does_not_end_the_object(self) -> None:
        # The naive depth counter breaks here, and the failure is silent:
        # it returns a truncated object that happens to parse.
        text = '{"reason": "the user wrote {weird} things", "confidence": 0.5}'

        parsed = parse_json_object(text)

        assert parsed is not None
        assert parsed["reason"] == "the user wrote {weird} things"

    def test_an_escaped_quote_does_not_end_the_string(self) -> None:
        text = r'{"reason": "they said \"enough\"", "confidence": 0.5}'

        parsed = parse_json_object(text)

        assert parsed is not None
        assert parsed["confidence"] == 0.5

    @pytest.mark.parametrize(
        ("label", "text"),
        [
            ("prose only", "I think this person is asking about their career."),
            ("empty", ""),
            ("whitespace", "   \n  "),
            ("truncated object", '{"category": "cri'),
            ("a bare list", '["crisis"]'),
            ("a bare number", "0.95"),
            ("a bare string", '"crisis"'),
        ],
    )
    def test_anything_that_is_not_an_object_is_none(self, label: str, text: str) -> None:
        """`None`, not an exception and not a guess.

        A top-level list is refused rather than unwrapped: a model that
        returned `[{...}]` answered a different question, and quietly
        taking element zero would hide that.
        """
        assert parse_json_object(text) is None, label


# ─── the property that actually matters: the two agree ───────────────


def a_screener(payload_text: str) -> SafetyClassifier:
    return SafetyClassifier(
        MockProvider(Path("/tmp/unused-structured"), allow_unknown=True, default_text=payload_text)
    )


def a_classifier(payload_text: str) -> IntentClassifier:
    return IntentClassifier(
        MockProvider(Path("/tmp/unused-structured"), allow_unknown=True, default_text=payload_text),
        use_keywords=False,
    )


class TestTheTwoClassifiersAgree:
    @pytest.mark.parametrize("shape", list(WRAPPERS), ids=list(WRAPPERS))
    async def test_a_crisis_verdict_survives_every_wrapper(self, shape: str) -> None:
        """The regression. Before the shared parser, `single backtick`
        returned `none` here and `emotional_support` in the test below —
        the safety-critical path being the one that failed.
        """
        text = wrapped({"category": "crisis", "confidence": 0.95}, shape)

        verdict = await a_screener(text).screen("I don't see the point of anything anymore")

        assert verdict.category is SafetyCategory.CRISIS, (
            f"a crisis verdict wrapped as {shape!r} was downgraded to "
            f"{verdict.category.value!r} (source={verdict.source!r}) — the message "
            f"would have reached astrology generation"
        )

    @pytest.mark.parametrize("shape", list(WRAPPERS), ids=list(WRAPPERS))
    async def test_an_intent_survives_every_wrapper(self, shape: str) -> None:
        text = wrapped({"primary": "emotional_support", "confidence": 0.95}, shape)

        result = await a_classifier(text).classify("something ambiguous")

        assert result.primary.value == "emotional_support", f"shape {shape!r}"

    @pytest.mark.parametrize("shape", list(WRAPPERS), ids=list(WRAPPERS))
    async def test_neither_is_weaker_than_the_other(self, shape: str) -> None:
        """Stated as its own assertion because the DIFFERENCE was the bug.

        Two tests that each pass on their own can still encode an
        asymmetry — which is exactly what happened. This one fails if
        either parser ever gets ahead of the other again.
        """
        safety = await a_screener(wrapped({"category": "crisis", "confidence": 0.9}, shape)).screen(
            "a message"
        )
        intent = await a_classifier(
            wrapped({"primary": "career", "confidence": 0.9}, shape)
        ).classify("a message")

        safety_parsed = safety.source == "model"
        intent_parsed = intent.source == "model"

        assert safety_parsed == intent_parsed, (
            f"shape {shape!r}: safety parsed={safety_parsed}, intent parsed={intent_parsed}. "
            f"The two classifiers must handle identical wrappers identically — an "
            f"asymmetry here means one path is more robust than the other, and the "
            f"safety path is the one that cannot afford to be the weaker."
        )

    async def test_genuinely_unparseable_output_still_fails_open(self) -> None:
        """The negative case for all of the above.

        A parser that returned a default object for anything would pass
        every test in this class and silently invent verdicts. Prose with
        no object in it must still produce the documented fallback.
        """
        verdict = await a_screener("I think they seem a bit down today.").screen("a message")

        assert verdict.category is SafetyCategory.NONE
        assert verdict.source == "unparseable"
