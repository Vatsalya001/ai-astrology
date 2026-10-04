"""`POST /v1/chat` — the SSE endpoint. PHASE-05 task 5.11.

§6 puts this behind a Go proxy that re-streams to the browser. That shape
is why this route does almost nothing: it builds the orchestrator from
the same factories `/v1/complete` uses and turns an async iterator of
events into an SSE body.

── Why the outcome comes back in a trailer event and not a return value ──

An HTTP response cannot return a value after its body. The Go proxy has
to persist the accumulated text, the telemetry and the partial flag in
one transaction — §15's "every AI call logged by Go" — and it needs them
AFTER the stream, which is exactly when there is nowhere left to put
them.

So the final `done` or `error` event carries everything the proxy needs,
and the proxy reads it off the stream rather than making a second call.
A second call would be a second chance to fail after the money was
already spent.
"""

from __future__ import annotations

import logging
from collections.abc import AsyncIterator
from datetime import UTC, datetime

from fastapi import APIRouter, Request
from fastapi.responses import StreamingResponse

from app.api.complete import get_orchestrator_parts
from app.orchestrator.events import ChatEvent, error_event
from app.orchestrator.streaming import ChatRequest, StreamingOrchestrator
from app.settings import settings

router = APIRouter(tags=["ai"], prefix="/v1")
log = logging.getLogger(__name__)


@router.post("/chat")
async def chat(body: ChatRequest, request: Request) -> StreamingResponse:
    """Stream one chat turn as Server-Sent Events.

    ── The headers, and why each one is load-bearing ──

    `text/event-stream` is the content type the SSE spec requires, and
    `EventSource` in the browser refuses anything else.

    `Cache-Control: no-cache` stops an intermediary serving a stale
    stream — which for SSE means replaying somebody else's answer.

    `X-Accel-Buffering: no` is the one that is easy to omit and
    expensive to omit. §6 names it: nginx buffers proxied responses by
    default, so without this the client receives the whole stream at
    once and "first token < 2 s" becomes "first token at the end". It is
    set HERE as well as in the Go proxy because whichever reverse proxy
    sits in front reads the header from whatever it is proxying, and
    §14 lists "SSE buffering breaks streaming in production" as a risk
    whose mitigation is to "test through the real reverse proxy, not
    just locally".
    """
    parts = get_orchestrator_parts()

    orchestrator = StreamingOrchestrator(
        provider=parts.provider,
        classifier=parts.classifier,
        screener=parts.screener,
        router=parts.router,
        prompt_version=settings.prompt_version_chat,
    )

    trace_id = getattr(request.state, "trace_id", "")

    return StreamingResponse(
        _encode(orchestrator, body, trace_id=trace_id),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


async def _encode(
    orchestrator: StreamingOrchestrator, body: ChatRequest, *, trace_id: str
) -> AsyncIterator[str]:
    """Events to wire bytes, with the outcome appended to the terminal one.

    ── Why the exception handler yields rather than raises ──

    A `StreamingResponse` has already sent its status line and headers by
    the time the generator runs. Raising from here cannot produce a 500 —
    it truncates the body, and the client sees a stream that simply stops
    with no terminal event. That is indistinguishable from a network
    failure, so a client retrying on truncation would retry a request
    that failed deterministically.

    Yielding an `error` event instead means every stream ends in a named
    event, which is what lets the proxy and the client tell "it broke"
    from "the connection died".
    """
    try:
        async for event in orchestrator.stream(body, now=datetime.now(UTC), trace_id=trace_id):
            yield _with_outcome(event, orchestrator).encode()

    except Exception as err:
        # Type only, never the message. A vendor error string can carry
        # the request URL, and for a hosted provider the key travels in
        # it — `.claude/rules/security.md`.
        log.exception("chat stream failed", extra={"trace_id": trace_id})
        yield error_event("INTERNAL", retryable=False, detail=type(err).__name__).encode()


def _with_outcome(event: ChatEvent, orchestrator: StreamingOrchestrator) -> ChatEvent:
    """Attach the outcome to a terminal event.

    Only `done` and `error` carry it, because only they are terminal and
    the outcome is not complete before then. Attaching it to every event
    would put the accumulated text on the wire once per token, which on a
    400-word answer is quadratic in the response length.
    """
    if event.type.value not in ("done", "error"):
        return event

    outcome = orchestrator.outcome
    return ChatEvent(
        type=event.type,
        data={
            **event.data,
            "outcome": {
                "text": outcome.text,
                "intent": outcome.intent.value,
                "is_crisis_response": outcome.is_crisis_response,
                "blocked": outcome.blocked,
                "declined": outcome.declined,
                "is_partial": outcome.is_partial,
                "context_version": outcome.context_version,
                "prompt_version": outcome.prompt_version,
                "basis": outcome.basis,
                # Integers. Invariant 4: money is an integer, and
                # `cost_micros` crossing a service boundary as a float is
                # the mistake that invariant exists to prevent.
                "usage": {
                    "input_tokens": outcome.usage.input_tokens,
                    "output_tokens": outcome.usage.output_tokens,
                    "cached_tokens": outcome.usage.cached_input_tokens,
                    "cost_micros": outcome.usage.cost_micros,
                },
            },
        },
    )
