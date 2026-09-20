"""Cost arithmetic. Integer, and loud about gaps.

`.claude/CLAUDE.md` invariant 4: money is an integer, never a float,
anywhere. These tests are the enforcement of that for the AI bill — the
ledger this eventually feeds is append-only, so a cost recorded wrong is
corrected with an opposing entry rather than edited away.
"""

from __future__ import annotations

import pytest

from app import pricing
from app.pricing import PRICES, UnpricedModelError, cost_micros, is_priced, priced
from app.providers.base import Usage
from app.settings import Settings


class TestTheUnpricedModelIsLoud:
    def test_an_unknown_model_raises(self) -> None:
        """Rather than costing nothing.

        The zero default is the expensive one: a model added by an admin
        override and never priced runs for months showing no cost on the
        dashboard, and the gap first appears on an invoice nobody can
        reconcile against anything.
        """
        with pytest.raises(UnpricedModelError, match="no price for model"):
            cost_micros("gpt-9-turbo", Usage(input_tokens=1000))

    def test_free_and_unpriced_are_different_facts(self) -> None:
        """A local model costs zero. An unknown model costs unknown.

        Collapsing them is what makes the zero default dangerous — the
        dashboard cannot then tell "free by design" from "we forgot".
        """
        assert cost_micros("qwen2.5:7b", Usage(input_tokens=10_000, output_tokens=5_000)) == 0
        assert is_priced("qwen2.5:7b") is True
        assert is_priced("gpt-9-turbo") is False

    def test_the_error_names_the_file_to_edit(self) -> None:
        # The recovery is a one-line edit to a committed JSON file. An
        # error that does not say so sends the reader into the adapters.
        with pytest.raises(UnpricedModelError, match=r"pricing\.json"):
            cost_micros("nothing", Usage())


class TestIntegerArithmetic:
    def test_the_result_is_always_an_int(self) -> None:
        cost = cost_micros(
            "claude-sonnet-5",
            Usage(input_tokens=1234, output_tokens=567, cached_input_tokens=89),
        )

        assert isinstance(cost, int)
        assert not isinstance(cost, float)

    @pytest.mark.parametrize(("tokens", "expected"), [(3, 1), (15, 5), (35, 11), (1005, 302)])
    def test_a_sub_micro_rate_is_computed_exactly(self, tokens: int, expected: int) -> None:
        """Cache reads are $0.30/MTok — 0.3 micros per token, a fraction.

        The obvious implementation computes a per-token rate and
        multiplies, which puts a float on the cheapest and
        highest-volume path in the service. `int()` or `round()` on the
        way out makes that invisible to any assertion about the return
        TYPE — which is what the first version of this test checked, and
        why it passed against a deliberately floating implementation.

        These four values are where the three candidate implementations
        disagree, so the assertion sees the arithmetic rather than the
        cast:

            tokens   exact   truncated   float+round
                 3       1           0             1
                15       5           4             4
                35      11          10            10
              1005     302         301           302

        15 and 35 land on a half-micro, where 0.3's binary
        representation and Python's round-half-to-EVEN both push the
        float answer down. Keeping the single division to the very end
        keeps every intermediate an integer and every one of these
        exact.
        """
        assert cost_micros("claude-sonnet-5", Usage(cached_input_tokens=tokens)) == expected

    def test_rounding_is_half_up_not_truncating(self) -> None:
        """Truncation loses up to a micro on every call, always downward.

        Systematic in one direction is the difference between noise and a
        bias, and the point of tracking cost is to notice when it grows.
        """
        assert cost_micros("claude-sonnet-5", Usage(cached_input_tokens=3)) == 1

    def test_the_exact_case_is_exact(self) -> None:
        # 1M input tokens at $3/MTok is exactly $3 = 3_000_000 micro-USD.
        assert cost_micros("claude-sonnet-5", Usage(input_tokens=1_000_000)) == 3_000_000

    def test_summing_is_order_independent(self) -> None:
        """The property a float cost does not have.

        Float addition does not associate: the same charges in a
        different order give a different total, which is indefensible on
        a bill and is exactly why the ledger rule exists.
        """
        calls = [
            Usage(input_tokens=1_111, output_tokens=333),
            Usage(input_tokens=7, cached_input_tokens=99_999),
            Usage(output_tokens=1, cache_write_input_tokens=13),
        ]

        forwards = sum(cost_micros("claude-sonnet-5", u) for u in calls)
        backwards = sum(cost_micros("claude-sonnet-5", u) for u in reversed(calls))

        assert forwards == backwards


class TestTheClassesArePricedDifferently:
    def test_a_cache_read_is_far_cheaper_than_fresh_input(self) -> None:
        """Roughly a tenth. This ratio is the whole point of caching.

        If the two priced the same, nothing downstream would notice a
        cache that silently stopped working — which PHASE-04 §15 lists as
        a named risk.
        """
        fresh = cost_micros("claude-sonnet-5", Usage(input_tokens=100_000))
        cached = cost_micros("claude-sonnet-5", Usage(cached_input_tokens=100_000))

        assert cached * 5 < fresh

    def test_a_cache_write_costs_more_than_fresh_input(self) -> None:
        """1.25x. The first request of a prefix is a small loss.

        Worth stating in a test because it is the counter-intuitive
        direction, and because a caching change that increases write
        volume without increasing reads makes the bill go UP.
        """
        fresh = cost_micros("claude-sonnet-5", Usage(input_tokens=100_000))
        write = cost_micros("claude-sonnet-5", Usage(cache_write_input_tokens=100_000))

        assert write > fresh

    def test_output_costs_more_than_input(self) -> None:
        assert cost_micros("claude-sonnet-5", Usage(output_tokens=1000)) > cost_micros(
            "claude-sonnet-5", Usage(input_tokens=1000)
        )

    def test_every_class_contributes(self) -> None:
        """The whole-request sum, so a dropped term is visible.

        A version of this that only asserted the total is positive would
        pass with three of the four terms missing.
        """
        usage = Usage(
            input_tokens=1_000_000,
            output_tokens=1_000_000,
            cached_input_tokens=1_000_000,
            cache_write_input_tokens=1_000_000,
        )

        assert cost_micros("claude-sonnet-5", usage) == (
            3_000_000 + 15_000_000 + 300_000 + 3_750_000
        )


class TestPricedHelper:
    def test_it_returns_a_copy(self) -> None:
        """Rather than mutating in place.

        The telemetry envelope is assembled from several layers; an
        in-place update would make the recorded cost depend on whether
        something else had already read the object.
        """
        original = Usage(input_tokens=1_000_000)

        result = priced("claude-sonnet-5", original)

        assert result.cost_micros == 3_000_000
        assert original.cost_micros == 0

    def test_it_preserves_every_other_field(self) -> None:
        usage = Usage(
            input_tokens=10, output_tokens=20, cached_input_tokens=30, cache_write_input_tokens=40
        )

        result = priced("claude-sonnet-5", usage)

        assert result.model_dump(exclude={"cost_micros"}) == usage.model_dump(
            exclude={"cost_micros"}
        )


class TestTheTableItself:
    def test_the_default_configured_models_are_all_priced(self) -> None:
        """The guard that catches a model swap without a price.

        Changing `LLM_MODEL_CHAT` is meant to be a config change with no
        deploy. That is only safe if the new model is priced — otherwise
        the first request after the change raises, in production, at the
        moment someone was trying to save money.
        """
        defaults = Settings(_env_file=None)  # type: ignore[call-arg]

        for model in (defaults.llm_model_fast, defaults.llm_model_chat, defaults.llm_model_deep):
            assert is_priced(model), (
                f"{model} is configured as a default in settings.py but has no entry in "
                f"pricing.json, so the first request using it raises UnpricedModelError"
            )

        assert is_priced(defaults.embedding_model)

    def test_no_price_is_negative(self) -> None:
        # A negative rate is always a transcription slip and would
        # produce a credit rather than a charge.
        for model, price in PRICES.items():
            assert min(price.input, price.output, price.cache_write, price.cache_read) >= 0, model

    def test_a_duplicate_model_name_is_rejected_at_load(self) -> None:
        """Two prices for one name means the cost depends on dict order.

        Not a thing a bill may depend on. The same model can legitimately
        be served by two vendors at different prices — and when that
        happens the table must be restructured deliberately rather than
        resolved by whichever key JSON happened to parse last.
        """
        original = pricing.PRICING_FILE

        try:
            pricing.PRICING_FILE = original.parent / "pricing.json"
            duplicated = {
                "a": {"m": {"input": 1, "output": 1, "cache_write": 1, "cache_read": 1}},
                "b": {"m": {"input": 2, "output": 2, "cache_write": 2, "cache_read": 2}},
            }
            import json
            import tempfile
            from pathlib import Path

            with tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp) / "pricing.json"
                path.write_text(json.dumps(duplicated))
                pricing.PRICING_FILE = path

                with pytest.raises(ValueError, match="priced twice"):
                    pricing._load()
        finally:
            pricing.PRICING_FILE = original

    def test_comment_keys_are_not_loaded_as_models(self) -> None:
        # JSON has no comment syntax, so the table uses `_comment` keys.
        # Loading one as a model would make `is_priced("_comment")` true
        # and hide a real gap behind a plausible-looking entry.
        assert not any(name.startswith("_") for name in PRICES)
