"""Immutability, and the ordering that decides the bill."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.prompts import (
    BuilderOrderError,
    PromptBuilder,
    PromptModuleNotFoundError,
    all_modules,
    load_module,
)
from app.prompts.registry import ANTHROPIC_MIN_CACHEABLE_TOKENS

LOCKFILE = Path(__file__).parent.parent / "app" / "prompts" / "published.lock.json"


class TestImmutability:
    """ "Never edit a published version" — enforced, not asked for.

    A response records its `prompt_version`. That only means something if
    `v1` today is byte-for-byte `v1` in six months; otherwise the field
    is a number that points at nothing and "why did the AI say that?" is
    unanswerable.
    """

    def test_no_published_module_has_changed(self) -> None:
        locked: dict[str, str] = json.loads(LOCKFILE.read_text())
        current = {m.qualified: m.digest for m in all_modules()}

        changed = [
            name for name, digest in locked.items() if name in current and current[name] != digest
        ]

        assert changed == [], (
            f"these published prompt modules were edited in place: {changed}. A "
            f"published version is frozen — every response already logged against it "
            f"claims content that no longer exists, so no answer from before this "
            f"edit can be explained. Create the next version instead, and leave the "
            f"old file alone."
        )

    def test_no_published_module_was_deleted(self) -> None:
        # Deleting is editing, taken to its limit: a logged
        # `prompt_version` pointing at a file that is gone is worse than
        # one pointing at a file that changed.
        locked: dict[str, str] = json.loads(LOCKFILE.read_text())
        current = {m.qualified for m in all_modules()}

        missing = sorted(set(locked) - current)

        assert missing == [], f"published modules were deleted: {missing}"

    def test_a_new_module_must_be_added_to_the_lockfile(self) -> None:
        """Otherwise the guard silently stops covering it.

        A module that exists but is not locked is a module anybody can
        edit freely — the test above would never notice, because it only
        compares names it already knows.
        """
        locked = set(json.loads(LOCKFILE.read_text()))
        current = {m.qualified for m in all_modules()}

        unlocked = sorted(current - locked)

        assert unlocked == [], (
            f"these modules are not in published.lock.json: {unlocked}. Until they "
            f"are, nothing stops them being edited in place. Regenerate the lockfile."
        )

    def test_the_lockfile_is_not_empty(self) -> None:
        # The guard above passes trivially against an empty lockfile, and
        # an empty one is exactly what a bad regeneration produces.
        assert len(json.loads(LOCKFILE.read_text())) >= 8


class TestLoading:
    def test_a_module_loads(self) -> None:
        module = load_module("astrology_rules", "v1")

        assert module.qualified == "astrology_rules.v1"
        assert "sidereal" in module.content.lower()

    def test_a_missing_module_raises_rather_than_defaulting(self) -> None:
        """A silent default here sends a request with no safety rules.

        That is the whole reason this is a hard error: the failure mode
        of a missing prompt module is not a worse answer, it is an
        unguarded one.
        """
        with pytest.raises(PromptModuleNotFoundError):
            load_module("astrology_rules", "v99")

    def test_traversal_is_refused_not_sanitised(self) -> None:
        # A prompt name is never user input, so a traversal in one is a
        # bug. Stripping it would hide the bug rather than surface it.
        with pytest.raises(PromptModuleNotFoundError, match="traversal"):
            load_module("../../../etc/passwd", "v1")


class TestOrdering:
    """The single biggest cost lever in the service.

    Caching matches a byte-identical prefix. Stable first, volatile last;
    one byte changing early invalidates everything downstream.
    """

    def test_a_stable_module_cannot_be_added_after_the_breakpoint(self) -> None:
        """Structural, not advisory.

        "Remember to put stable content first" is exactly the kind of
        instruction that survives review and not the next refactor — and
        when it is forgotten, nothing fails. The cost just doubles.
        """
        builder = PromptBuilder().add("system_base", "v1").cache_breakpoint()

        with pytest.raises(BuilderOrderError, match="after the cache breakpoint"):
            builder.add("astrology_rules", "v1")

    def test_volatile_content_cannot_go_before_the_breakpoint(self) -> None:
        # The mirror image, and the more expensive direction: volatile
        # content inside the cached prefix changes the prefix on every
        # request, so nothing is ever a hit.
        builder = PromptBuilder().add("system_base", "v1")

        with pytest.raises(BuilderOrderError, match="before the cache breakpoint"):
            builder.chart_context("Moon in Sagittarius")

    def test_build_without_a_breakpoint_is_refused(self) -> None:
        builder = PromptBuilder().add("system_base", "v1")

        with pytest.raises(BuilderOrderError, match="without a cache_breakpoint"):
            builder.build()

    def test_a_breakpoint_at_position_zero_is_refused(self) -> None:
        # It caches nothing, and costs a round trip to discover that.
        with pytest.raises(BuilderOrderError, match="nothing before it"):
            PromptBuilder().cache_breakpoint()

    def test_stable_blocks_are_marked_cacheable_and_volatile_ones_are_not(self) -> None:
        blocks = (
            PromptBuilder()
            .add("system_base", "v1")
            .cache_breakpoint()
            .chart_context("Moon in Sagittarius")
            .build()
        )

        assert [b.cacheable for b in blocks] == [True, False]

    def test_an_empty_volatile_block_is_dropped(self) -> None:
        """An empty block still changes the prefix if it is emitted.

        A first-turn conversation has no summary. Emitting an empty
        string for it makes turn one's prompt structurally different from
        turn two's, for no content.
        """
        blocks = (
            PromptBuilder()
            .add("system_base", "v1")
            .cache_breakpoint()
            .conversation_context("", "")
            .build()
        )

        assert len(blocks) == 1


class TestPrefixStability:
    def test_two_identical_builds_produce_a_byte_identical_prefix(self) -> None:
        """The gate item, stated as an equality.

        If this ever fails, caching is off and nobody will notice from
        the outside: responses stay correct and the bill quietly
        multiplies.
        """

        def build() -> str:
            return (
                PromptBuilder()
                .add("system_base", "v1")
                .add("astrology_rules", "v1")
                .add("safety_rules", "v1")
                .persona("vedic_guide", "v1")
                .cache_breakpoint()
                .cacheable_prefix
            )

        assert build() == build()

    def test_changing_only_volatile_content_leaves_the_prefix_untouched(self) -> None:
        """The property that makes caching worth anything.

        Two users with different charts must share a prefix. If they do
        not, the cache hit rate is zero however stable each user's own
        prompt is.
        """

        def prefix_for(chart: str) -> str:
            builder = (
                PromptBuilder()
                .add("system_base", "v1")
                .add("astrology_rules", "v1")
                .cache_breakpoint()
            )
            builder.chart_context(chart)
            return builder.cacheable_prefix

        assert prefix_for("Moon in Sagittarius") == prefix_for("Sun in Aquarius")

    def test_changing_a_stable_module_does_change_the_prefix(self) -> None:
        # The negative case. A "stability" test that passed regardless of
        # content would be asserting that a constant equals itself.
        with_persona = (
            PromptBuilder()
            .add("system_base", "v1")
            .persona("vedic_guide", "v1")
            .cache_breakpoint()
            .cacheable_prefix
        )
        without = PromptBuilder().add("system_base", "v1").cache_breakpoint().cacheable_prefix

        assert with_persona != without


def test_the_versions_used_are_recorded() -> None:
    """ "Why did the AI say that?" needs the prompt, not just the model."""
    builder = (
        PromptBuilder().add("system_base", "v1").persona("vedic_guide", "v1").cache_breakpoint()
    )

    assert builder.versions == {
        "system_base": "v1",
        "personas/vedic_guide": "v1",
    }


# ─── whether the breakpoint actually engages ─────────────────────────


def test_the_shipped_prefix_is_still_below_anthropics_minimum() -> None:
    """Pins a fact that is currently uncomfortable, so it cannot change
    without anyone noticing.

    Anthropic ignores `cache_control` below ~1024 tokens (2048 on
    Haiku). Today's five-module prefix is ~770, so the breakpoint the
    whole builder is built around is a NO-OP on every tier — no write,
    no read, and no error saying so.

    §15 names "prompt caching silently stops working in prod" as a risk.
    A cache that never STARTED working is indistinguishable from the
    outside, and the only thing that would have surfaced it is a
    measurement. This is that measurement.

    When Phase 5's RAG corpus pushes the prefix over the line, this test
    fails — which is the point. Flip the assertion then, and know the
    date the cache began paying for itself.
    """
    builder = (
        PromptBuilder()
        .add("system_base", "v1")
        .add("astrology_rules", "v1")
        .add("safety_rules", "v1")
        .persona("vedic_guide", "v1")
        .add("output_format", "v1")
        .cache_breakpoint()
    )

    tokens = builder.cacheable_prefix_tokens

    assert tokens < ANTHROPIC_MIN_CACHEABLE_TOKENS, (
        f"the cacheable prefix is now ~{tokens} tokens, at or above Anthropic's "
        f"~{ANTHROPIC_MIN_CACHEABLE_TOKENS}-token minimum. The breakpoint has begun "
        f"to engage — which is good news. Flip this assertion to >= and record the "
        f"date in docs/PROJECT_STATUS.md."
    )
    # A floor as well, so the prefix cannot quietly SHRINK. A shorter
    # prompt is a cheaper request and a worse answer, and nothing else
    # would report it.
    assert tokens > 500, f"the stable prefix collapsed to ~{tokens} tokens"


def test_every_persona_produces_a_similar_prefix_length() -> None:
    """They differ by one module, so they should differ by little.

    A persona far longer than the others would cross the caching
    threshold on its own, making the cache engage for some users and not
    others — the hardest kind of cost anomaly to diagnose, because the
    answers are all correct.
    """
    lengths = {}
    for persona in ("vedic_guide", "career_guide", "relationship_guide", "spiritual_guide"):
        builder = (
            PromptBuilder()
            .add("system_base", "v1")
            .add("astrology_rules", "v1")
            .add("safety_rules", "v1")
            .persona(persona, "v1")
            .add("output_format", "v1")
            .cache_breakpoint()
        )
        lengths[persona] = builder.cacheable_prefix_tokens

    spread = max(lengths.values()) - min(lengths.values())
    assert spread < 200, f"personas differ by ~{spread} tokens: {lengths}"
