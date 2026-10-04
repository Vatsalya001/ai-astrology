"""The SSE wire format. PHASE-05 §6.

Split out of `test_chat_stream.py` because these are the only
SYNCHRONOUS tests in that area — `events.py` encodes a value and returns
bytes, with nothing to await. Left in a module carrying a blanket
`pytest.mark.asyncio`, each one warned that it was marked async and was
not, which is noise that trains people to ignore warnings.
"""

from __future__ import annotations

import json

from app.orchestrator.events import (
    EXPECTED_ORDER,
    ChatEvent,
    EventType,
    error_event,
)


def test_the_documented_event_order_is_the_one_in_code() -> None:
    """§6's sequence, as data.

    `EXPECTED_ORDER` is what `test_chat_stream.py` asserts the stream
    against, so if it drifted from §6 the stream test would keep passing
    while checking the wrong thing.
    """
    assert [event.value for event in EXPECTED_ORDER] == [
        "intent",
        "context",
        "token",
        "explanation",
        "followups",
        "done",
    ]


def test_every_event_name_is_one_of_the_seven() -> None:
    """The Go proxy switches on these strings and the web client switches
    on them again, so a rename that compiled would break two services
    silently."""
    assert {event.value for event in EventType} == {
        "intent",
        "context",
        "token",
        "explanation",
        "followups",
        "done",
        "error",
    }


def test_an_event_encodes_as_valid_sse() -> None:
    event = ChatEvent(type=EventType.TOKEN, data={"text": "Your "})

    wire = event.encode()

    assert wire == 'event: token\ndata: {"text":"Your "}\n\n'
    assert wire.endswith("\n\n"), (
        "the blank line is the event delimiter; without it every event is the "
        "beginning of the next one"
    )


def test_a_newline_in_the_payload_cannot_terminate_the_event_early() -> None:
    """SSE is line-oriented. A raw newline inside `data:` ends the event,
    and the client sees truncated JSON followed by a fragment that parses
    as nothing."""
    event = ChatEvent(type=EventType.TOKEN, data={"text": "line one\nline two"})

    wire = event.encode()
    body = wire.split("data: ", 1)[1].rstrip("\n")

    assert "\n" not in body
    assert json.loads(body)["text"] == "line one\nline two"


def test_devanagari_does_not_reach_the_wire_as_raw_bytes() -> None:
    """`ensure_ascii` keeps multi-byte characters escaped, because a
    proxy that re-chunks on bytes can split one across two reads and
    produce an invalid UTF-8 fragment."""
    event = ChatEvent(type=EventType.TOKEN, data={"text": "शनि"})

    wire = event.encode()

    assert "शनि" not in wire
    assert json.loads(wire.split("data: ", 1)[1])["text"] == "शनि"


def test_the_error_event_never_carries_a_long_vendor_string() -> None:
    """A vendor error string can contain the request URL, and for a
    hosted provider the key travels in it."""
    event = error_event("AI_UNAVAILABLE", retryable=True, detail="x" * 5000)

    assert len(event.data["detail"]) <= 200
