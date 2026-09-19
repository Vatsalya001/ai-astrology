"""The provider CI runs on.

Its correctness matters more than most: every other test in this phase
believes what it says. A mock that silently invents answers turns the
whole suite into theatre.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from app.providers import (
    CompletionRequest,
    Message,
    MockProvider,
    ProviderError,
    RequestMetadata,
    SystemBlock,
    request_fingerprint,
)


def a_request(
    text: str = "what does my chart say",
    *,
    system: list[str] | None = None,
    tier: str = "chat",
    trace: str = "t-1",
) -> CompletionRequest:
    return CompletionRequest(
        messages=[Message(role="user", content=text)],
        system=[SystemBlock(content=s) for s in (system or [])],
        tier=tier,  # type: ignore[arg-type]
        metadata=RequestMetadata(trace_id=trace),
    )


class TestTheFingerprint:
    """What must and must not change the answer."""

    def test_the_same_question_fingerprints_the_same(self) -> None:
        assert request_fingerprint(a_request()) == request_fingerprint(a_request())

    def test_a_different_question_fingerprints_differently(self) -> None:
        assert request_fingerprint(a_request("a")) != request_fingerprint(a_request("b"))

    def test_trace_id_does_not_change_the_fingerprint(self) -> None:
        """The decision that makes the fixture set usable at all.

        `trace_id` is unique per request by construction. Including it in
        the hash would give every call its own fingerprint, so no fixture
        would ever match twice and the mock would be a very slow way of
        always raising.
        """
        assert request_fingerprint(a_request(trace="t-1")) == request_fingerprint(
            a_request(trace="t-2")
        )

    def test_the_system_prompt_does_change_the_fingerprint(self) -> None:
        """It changes the answer in reality, so it must change the key.

        Two requests with the same user message and different system
        prompts are different questions — one carries the safety rules
        and one does not — and sharing a fixture between them would let a
        prompt-regression test pass against the wrong recording.
        """
        assert request_fingerprint(a_request(system=["rules v1"])) != request_fingerprint(
            a_request(system=["rules v2"])
        )

    def test_the_tier_changes_the_fingerprint(self) -> None:
        assert request_fingerprint(a_request(tier="fast")) != request_fingerprint(
            a_request(tier="deep")
        )


class TestUnknownRequests:
    async def test_an_unknown_request_raises_rather_than_inventing(self, tmp_path: Path) -> None:
        """The single most important behaviour in this file.

        A plausible default would let a test pass while asserting nothing
        about the request it actually made — which has happened six times
        in this codebase's short history, every time because something
        returned a benign value where it should have refused.
        """
        provider = MockProvider(tmp_path)

        with pytest.raises(ProviderError, match="no fixture"):
            await provider.complete(a_request())

    async def test_the_error_says_how_to_fix_it(self, tmp_path: Path) -> None:
        # A failure a reader cannot act on costs more than it saves.
        provider = MockProvider(tmp_path)

        with pytest.raises(ProviderError) as caught:
            await provider.complete(a_request())

        message = str(caught.value)
        assert "record" in message.lower()
        assert "allow_unknown" in message

    async def test_allow_unknown_exists_for_tests_that_do_not_care(self, tmp_path: Path) -> None:
        """Retry and circuit-breaker tests should not need a fixture each.

        The escape hatch is opt-in and named, so choosing it is visible
        in the test that chose it.
        """
        provider = MockProvider(tmp_path, allow_unknown=True, default_text="anything")

        response = await provider.complete(a_request())

        assert response.text == "anything"


class TestFixtures:
    async def test_a_recorded_fixture_is_returned(self, tmp_path: Path) -> None:
        provider = MockProvider(tmp_path)
        req = a_request()
        provider.record(req, "Jupiter is exalted in Cancer.")

        response = await provider.complete(req)

        assert response.text == "Jupiter is exalted in Cancer."
        assert response.provider_id == "mock"

    async def test_fixtures_are_matched_by_content_not_by_call_order(self, tmp_path: Path) -> None:
        """The reason this is hashed rather than queued.

        A queue of canned replies makes every assertion depend on how many
        calls the code under test happens to make, so inserting one
        classification step silently shifts every later assertion onto the
        wrong fixture — and the test still passes, against the wrong
        recording.
        """
        provider = MockProvider(tmp_path)
        first, second = a_request("question one"), a_request("question two")
        provider.record(first, "answer one")
        provider.record(second, "answer two")

        # Deliberately out of recording order, and repeated.
        assert (await provider.complete(second)).text == "answer two"
        assert (await provider.complete(first)).text == "answer one"
        assert (await provider.complete(second)).text == "answer two"

    async def test_a_recorded_refusal_survives_the_round_trip(self, tmp_path: Path) -> None:
        # `refusal` is a distinct finish_reason for a reason (see base.py);
        # a mock that flattened it to "stop" would make the safety path
        # untestable.
        provider = MockProvider(tmp_path)
        req = a_request("something the model declines")
        provider.record(req, "I can't help with that.", finish_reason="refusal")

        assert (await provider.complete(req)).finish_reason == "refusal"


class TestWhatTheOrchestratorAsked:
    async def test_requests_are_recorded_in_order(self, tmp_path: Path) -> None:
        """Usually the more useful assertion.

        What an orchestrator ASKED is the thing worth checking — that the
        safety classifier ran first, that the prompt version was carried,
        that a crisis message never reached the astrology path at all.
        What it got back is a fixture somebody chose.
        """
        provider = MockProvider(tmp_path, allow_unknown=True)

        await provider.complete(a_request("first"))
        await provider.complete(a_request("second"))

        assert [r.messages[0].content for r in provider.requests] == ["first", "second"]

    async def test_fail_next_fires_once_and_then_clears(self, tmp_path: Path) -> None:
        """One shot, so "fails then succeeds" needs no counter in the test."""
        provider = MockProvider(tmp_path, allow_unknown=True, default_text="ok")
        provider.fail_next = ProviderError("boom", provider_id="mock", retryable=True)

        with pytest.raises(ProviderError):
            await provider.complete(a_request())

        assert (await provider.complete(a_request())).text == "ok"


class TestStreaming:
    async def test_the_stream_reassembles_into_the_full_text(self, tmp_path: Path) -> None:
        provider = MockProvider(tmp_path)
        req = a_request()
        provider.record(req, "Saturn is in your ninth house")

        chunks = [chunk async for chunk in provider.stream(req)]

        assert "".join(c.text for c in chunks) == "Saturn is in your ninth house"

    async def test_it_yields_more_than_one_chunk(self, tmp_path: Path) -> None:
        """Otherwise the consumer's join is never exercised.

        A mock that delivers the whole response as a single chunk lets a
        broken assembler pass: the bug lives in the joining, and a
        one-chunk stream never joins anything.
        """
        provider = MockProvider(tmp_path)
        req = a_request()
        provider.record(req, "Saturn is in your ninth house")

        chunks = [chunk async for chunk in provider.stream(req)]

        assert len(chunks) > 1

    async def test_usage_arrives_only_on_the_final_chunk(self, tmp_path: Path) -> None:
        """A per-chunk usage would be summed into a wrong total.

        Real providers report usage once, at the end. A mock that
        attached it to every chunk would let a consumer that sums as it
        goes look correct here and over-bill in production.
        """
        provider = MockProvider(tmp_path)
        req = a_request()
        provider.record(req, "one two three")

        chunks = [chunk async for chunk in provider.stream(req)]

        assert [c.usage is not None for c in chunks] == [False, False, True]
        assert chunks[-1].finish_reason == "stop"


async def test_the_mock_is_local_tier_so_production_refuses_it(tmp_path: Path) -> None:
    """Not `paid`, deliberately.

    A mock registered in production would serve canned astrology to
    people who asked a real question. The PII guard already refuses any
    non-paid tier there, so declaring `local` makes that guard cover this
    case for free.
    """
    assert MockProvider(tmp_path).tier == "local"
