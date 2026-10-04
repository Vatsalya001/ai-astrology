"""The SSE event sequence, as data. PHASE-05 §6.

Separated from the streaming orchestrator so the EVENTS can be asserted
without running a model, and so the Go proxy's contract is a thing with a
name rather than a shape inferred from a handler.

── The order is the design ──

§6: "deliberately front-loaded so the UI has something to show
immediately".

    intent  →  context  →  token…  →  explanation  →  followups  →  done

`intent` and `context` arrive before the first token because the UI can
render "looking at your 10th house, Saturn, and your current Jupiter
period" while the model is still thinking. That turns two seconds of
waiting into two seconds of visible work, and it is free: both are
already computed before generation starts.

── The tension §6 does not address ──

**A streamed response cannot be validated before the user has seen it.**

PHASE-04's non-streaming path generates, validates, and regenerates once
with a corrective instruction if the output claims a fact the chart does
not hold. That sequence is impossible here: by the time a fabricated
placement is visible to the validator, it is also visible to the user.

Four options, and none is free:

  1. Buffer the whole response, validate, then emit. Correct, and it
     deletes streaming — §15 budgets "first token < 2 s" and a buffered
     response's first token is its last.
  2. Stream everything, validate at the end. The user has already read
     the wrong placement. The validator becomes a logging facility.
  3. Hold the first token until the first sentence validates. Preserves
     the guarantee and spends most of the 2-second budget before
     anything appears.
  4. Stream, and validate each sentence AS IT COMPLETES, cutting the
     stream the moment a violation appears.

This ships (4). It is the only one that keeps both properties, and the
cost is stated rather than hidden: **a blocked response may have shown
the user up to one sentence of invalid text.** What it cannot do is
complete — the stream ends in `error`, not `done`, the client renders a
blocked state, and the message is persisted with `is_partial` set.

That is a real weakening against the non-streaming path and it is the
right trade at this budget. The alternative is a product whose first
token arrives after its last.
"""

from __future__ import annotations

import json
from enum import StrEnum
from typing import Any

from pydantic import BaseModel, Field


class EventType(StrEnum):
    """The six event names from §6, plus `error`.

    `StrEnum` so the wire name and the Python name cannot drift: the Go
    proxy switches on these strings and the web client switches on them
    again, so a rename that compiled would break two services silently.
    """

    INTENT = "intent"
    CONTEXT = "context"
    TOKEN = "token"
    EXPLANATION = "explanation"
    FOLLOWUPS = "followups"
    DONE = "done"
    ERROR = "error"


# The order §6 specifies, as data, so it can be asserted.
#
# `token` repeats and `error` can replace everything from any point, so
# this is the order of FIRST occurrence for the events that appear at
# most once.
EXPECTED_ORDER: tuple[EventType, ...] = (
    EventType.INTENT,
    EventType.CONTEXT,
    EventType.TOKEN,
    EventType.EXPLANATION,
    EventType.FOLLOWUPS,
    EventType.DONE,
)


class ChatEvent(BaseModel):
    """One SSE event. Serialised by `encode`, never by hand."""

    type: EventType
    data: dict[str, Any] = Field(default_factory=dict)

    def encode(self) -> str:
        """The wire form: `event: <name>\\ndata: <json>\\n\\n`.

        ── Why the JSON is forced onto one line ──

        A newline inside the `data:` payload would terminate the event
        early, and the client would see a truncated JSON document
        followed by a fragment that parses as nothing. `json.dumps`
        without `indent` never emits one, and `ensure_ascii` keeps
        Devanagari out of the raw bytes — which matters because SSE is
        line-oriented over a byte stream and a proxy that re-chunks on
        bytes can split a multi-byte character across two reads.

        The blank line at the end is not cosmetic: it is the event
        delimiter. Omitting it makes every event the beginning of the
        next one.
        """
        payload = json.dumps(self.data, separators=(",", ":"), ensure_ascii=True)
        return f"event: {self.type.value}\ndata: {payload}\n\n"


def intent_event(intent: str, confidence: float) -> ChatEvent:
    return ChatEvent(
        type=EventType.INTENT,
        data={"intent": intent, "confidence": round(confidence, 4)},
    )


def context_event(*, fact_count: int, chunk_count: int) -> ChatEvent:
    """§6: `{"fact_count":12,"chunk_count":6}`.

    COUNTS, not content. The context holds the user's placements and the
    retrieved passages, and `.claude/rules/security.md` keeps birth
    details out of anything a client logs or a proxy records. The
    explanation event carries the human-readable basis, which is
    deliberately the same information at a coarser grain.
    """
    return ChatEvent(
        type=EventType.CONTEXT,
        data={"fact_count": fact_count, "chunk_count": chunk_count},
    )


def token_event(text: str) -> ChatEvent:
    return ChatEvent(type=EventType.TOKEN, data={"text": text})


def explanation_event(basis: list[str]) -> ChatEvent:
    """§6: `{"basis":["10th house","Saturn","Jupiter Mahadasha",…]}`.

    This is what "Why am I seeing this?" renders, and §16 requires it to
    render "the real stored context" — so the basis is derived from what
    was actually selected and retrieved, never composed for display.
    """
    return ChatEvent(type=EventType.EXPLANATION, data={"basis": basis})


def followups_event(questions: list[str]) -> ChatEvent:
    return ChatEvent(type=EventType.FOLLOWUPS, data={"questions": questions})


def done_event(
    *,
    message_id: str = "",
    usage: dict[str, Any] | None = None,
    blocked: bool = False,
    is_crisis: bool = False,
    declined: bool = False,
) -> ChatEvent:
    """The terminal success event.

    `message_id` is empty from Python: ai-service does not write, so it
    cannot know the id. The Go proxy fills it in after persisting, which
    is also where §15's "every AI call logged by Go" is satisfied.
    """
    return ChatEvent(
        type=EventType.DONE,
        data={
            "message_id": message_id,
            "usage": usage or {},
            "blocked": blocked,
            "is_crisis_response": is_crisis,
            "declined": declined,
        },
    )


def error_event(code: str, *, retryable: bool, detail: str = "") -> ChatEvent:
    """§6: `{"code":"AI_UNAVAILABLE","retryable":true}`.

    `retryable` is the field the client branches on, so it decides
    whether the user sees "try again" or "something went wrong". It
    mirrors `ProviderError.retryable` rather than being re-derived, for
    the same reason the HTTP layer maps 5xx and 429 one way and 4xx
    another: a 4xx will fail identically forever and retrying it turns
    one bad request into several.

    `detail` is deliberately short and never carries the message or the
    chart. A vendor error string can contain the request URL, and for a
    hosted provider the key travels in it.
    """
    data: dict[str, Any] = {"code": code, "retryable": retryable}
    if detail:
        data["detail"] = detail[:200]
    return ChatEvent(type=EventType.ERROR, data=data)
