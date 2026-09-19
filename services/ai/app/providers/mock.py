"""The provider CI uses, and the only one it is allowed to use.

`.claude/rules/testing.md`: "CI must never call a language model. Tests
depending on a real model are slow, flaky, non-deterministic and
eventually expensive."

── Keyed by a hash of the request, not by call order ──

A queue of canned replies makes every test depend on how many calls the
code under test happens to make, so adding one classification step
silently shifts every later assertion onto the wrong fixture. Hashing
the request means a test gets the fixture for the question it actually
asked, in any order, however many times.

── An unknown request RAISES ──

The tempting alternative is to return a plausible default. It is the
wrong default, and this codebase has the scars: six times in the last
day a test passed while asserting nothing, every time because something
returned a benign value where it should have refused. A missing fixture
is a test that is not testing what its name claims, and it should say so
in the failure rather than in a subtlety nobody reads.

`allow_unknown=True` exists for the cases that genuinely do not care
what the model said — a retry test, a circuit-breaker test — where
demanding a fixture per generated request would be ceremony.
"""

from __future__ import annotations

import asyncio
import hashlib
import json
from collections.abc import AsyncIterator
from pathlib import Path

from app.providers.base import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    ProviderError,
    ProviderTier,
    Usage,
)


def request_fingerprint(req: CompletionRequest) -> str:
    """A stable short hash of everything that should change the answer.

    Deliberately EXCLUDES `metadata`: trace_id is different on every
    request by construction, and including it would give every call a
    unique fingerprint and make the fixture set unusable.

    Also excludes `max_tokens` and `temperature`. They change the answer
    in reality, but a fixture is a fixed answer already, and including
    them means a test that nudges a limit has to re-record a fixture
    whose content did not change.
    """
    payload = {
        "system": [block.content for block in req.system],
        "messages": [{"role": m.role, "content": m.content} for m in req.messages],
        "tier": req.tier,
        "json_schema": req.json_schema,
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()[:16]


class MockProvider:
    """Deterministic, offline, and loud about gaps."""

    def __init__(
        self,
        fixtures_dir: Path,
        *,
        provider_id: str = "mock",
        allow_unknown: bool = False,
        default_text: str = "",
        latency_ms: int = 0,
    ) -> None:
        self._fixtures_dir = fixtures_dir
        self._id = provider_id
        self._allow_unknown = allow_unknown
        self._default_text = default_text
        self._latency_ms = latency_ms

        self.requests: list[CompletionRequest] = []
        """Every request received, in order.

        Kept because the most useful assertion about an orchestrator is
        usually what it ASKED, not what it got back — that the safety
        classifier ran before the chat call, that the system prompt
        carried the right prompt version, that a crisis message never
        reached the astrology path at all.
        """

        self.fail_next: Exception | None = None
        """Set to make the next call raise, then clear itself.

        One shot rather than a permanent mode, so a retry test can assert
        "fails once, then succeeds" without a second provider or a
        mutable counter in the test body.
        """

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        # `local` and not `paid`: the PII guard must refuse to run this in
        # production. A mock serving real users would return canned
        # astrology to people who asked a real question.
        return "local"

    @property
    def capabilities(self) -> Capabilities:
        return Capabilities(streaming=True, json_mode=True, prompt_caching=False)

    # ─── fixtures ────────────────────────────────────────────────────

    def _fixture_path(self, fingerprint: str) -> Path:
        return self._fixtures_dir / f"{fingerprint}.json"

    def record(self, req: CompletionRequest, text: str, **extra: object) -> Path:
        """Write a fixture for a request. Used to seed the fixture set.

        Returns the path so a test that records can also assert on where
        it landed, and so the fingerprint is discoverable without
        reimplementing the hash.
        """
        self._fixtures_dir.mkdir(parents=True, exist_ok=True)
        path = self._fixture_path(request_fingerprint(req))
        body: dict[str, object] = {"text": text, "finish_reason": "stop", **extra}
        path.write_text(json.dumps(body, indent=1, ensure_ascii=False) + "\n")
        return path

    def _load(self, req: CompletionRequest) -> dict[str, object]:
        fingerprint = request_fingerprint(req)
        path = self._fixture_path(fingerprint)

        if path.exists():
            loaded: dict[str, object] = json.loads(path.read_text())
            return loaded

        if self._allow_unknown:
            return {"text": self._default_text, "finish_reason": "stop"}

        raise ProviderError(
            f"no fixture for request {fingerprint} at {path}. The mock refuses to "
            f"invent an answer: a plausible default would let this test pass while "
            f"asserting nothing about the request it actually made. Record one with "
            f"MockProvider.record(), or pass allow_unknown=True if this test does "
            f"not care what the model said.",
            provider_id=self._id,
            retryable=False,
        )

    # ─── the protocol ────────────────────────────────────────────────

    async def complete(self, req: CompletionRequest) -> CompletionResponse:
        self.requests.append(req)

        if self.fail_next is not None:
            failure, self.fail_next = self.fail_next, None
            raise failure

        fixture = self._load(req)

        if self._latency_ms:
            # Real, so a timeout test has something to time out against.
            await asyncio.sleep(self._latency_ms / 1000)

        text = str(fixture.get("text", ""))
        return CompletionResponse(
            text=text,
            finish_reason=fixture.get("finish_reason", "stop"),  # type: ignore[arg-type]
            usage=Usage(
                # Token counts a caller can assert on without a tokenizer.
                # Four characters per token is the usual rough figure and
                # is close enough for a fixture; nothing bills against it.
                input_tokens=sum(len(m.content) for m in req.messages) // 4,
                output_tokens=len(text) // 4,
                cost_micros=0,
            ),
            model=f"mock-{req.tier}",
            provider_id=self._id,
            latency_ms=self._latency_ms,
        )

    async def stream(self, req: CompletionRequest) -> AsyncIterator[CompletionChunk]:
        """Word by word, with the usage on the last chunk only.

        Chunked rather than delivered whole because the thing worth
        testing about a stream is the assembly — a consumer that only
        ever sees one chunk is not exercising the code that joins them,
        and the join is where the bugs are.
        """
        response = await self.complete(req)
        words = response.text.split(" ")

        for index, word in enumerate(words):
            last = index == len(words) - 1
            yield CompletionChunk(
                text=word if last else word + " ",
                finish_reason=response.finish_reason if last else None,
                usage=response.usage if last else None,
            )

    async def health_check(self) -> bool:
        return True
