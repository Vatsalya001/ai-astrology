"""Structured logging with PII redaction and trace propagation.

Mirrors the Go implementation in
services/api/internal/platform/logging so that all three services emit
the same field names and the same redaction guarantees.

Two things matter here:

  1. Redaction happens in the formatter, not at the call site. Relying
     on every developer to remember not to log an email is how emails
     end up in production logs.

  2. Every line carries a trace_id taken from a contextvar, set by
     middleware from the inbound X-Trace-Id header. A request in this
     system crosses three processes; without a correlating ID the logs
     are unreadable.

NOTE: this module is duplicated verbatim in services/astro. The two
services are independent deployables with independent dependency trees,
and a shared package would couple their release cycles for eighty lines
of code. If it grows much beyond this, reconsider.
"""

from __future__ import annotations

import json
import logging
import sys
from contextvars import ContextVar
from typing import Any

# ─── Trace context ───────────────────────────────────────────────────

_trace_id: ContextVar[str] = ContextVar("trace_id", default="")


def set_trace_id(value: str) -> None:
    _trace_id.set(value)


def get_trace_id() -> str:
    return _trace_id.get()


# ─── Redaction ───────────────────────────────────────────────────────

# Redacted wherever they appear as a key, at any nesting depth.
#
# Birth date, time and place are on this list deliberately: in
# combination they are close to a unique identifier for a person, which
# makes them PII in exactly the way an email address is.
SENSITIVE_KEYS = frozenset(
    {
        "email",
        "phone",
        "name",
        "display_name",
        "address",
        "date_of_birth",
        "birth_date",
        "time_of_birth",
        "birth_time",
        "place_of_birth",
        "birth_place",
        "latitude",
        "longitude",
        "password",
        "token",
        "access_token",
        "refresh_token",
        "api_key",
        "secret",
        "authorization",
        "internal_token",
        "key_secret",
        "webhook_secret",
        "signature",
        "otp",
        "code",
    }
)

REDACTED = "[REDACTED]"

# Bound on recursion depth. A pathological or cyclic structure must not
# be able to hang the logger.
_MAX_DEPTH = 6


def redact(value: Any, depth: int = 0) -> Any:
    """Recursively redact sensitive keys in a nested structure.

    Unlike Go's slog — which cannot descend into arbitrary values and so
    requires the "log IDs, not objects" discipline — Python can walk
    dicts and sequences. That makes this strictly stronger than the Go
    side, but the discipline still applies: a redacted object is a large
    log line for no benefit.
    """
    if depth >= _MAX_DEPTH:
        return "[TRUNCATED]"

    if isinstance(value, dict):
        return {
            k: (REDACTED if str(k).lower() in SENSITIVE_KEYS else redact(v, depth + 1))
            for k, v in value.items()
        }

    if isinstance(value, (list, tuple)):
        return [redact(v, depth + 1) for v in value]

    return value


# Attributes the stdlib puts on every LogRecord. Anything else was
# supplied by the caller via `extra=` and belongs in the output.
_STANDARD_ATTRS = frozenset(
    {
        "args",
        "asctime",
        "created",
        "exc_info",
        "exc_text",
        "filename",
        "funcName",
        "levelname",
        "levelno",
        "lineno",
        "module",
        "msecs",
        "message",
        "msg",
        "name",
        "pathname",
        "process",
        "processName",
        "relativeCreated",
        "stack_info",
        "thread",
        "threadName",
        "taskName",
    }
)


class JSONFormatter(logging.Formatter):
    """Emits one JSON object per line, matching the Go service's shape."""

    def __init__(self, service: str) -> None:
        super().__init__()
        self.service = service

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "time": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"),
            "level": record.levelname,
            "service": self.service,
            "msg": record.getMessage(),
        }

        if trace := get_trace_id():
            payload["trace_id"] = trace

        # Caller-supplied fields via extra=, redacted.
        for key, value in record.__dict__.items():
            if key in _STANDARD_ATTRS or key.startswith("_"):
                continue
            payload[key] = REDACTED if key.lower() in SENSITIVE_KEYS else redact(value)

        if record.exc_info:
            # The traceback goes to the log and never to a client.
            payload["error"] = self.formatException(record.exc_info)

        return json.dumps(payload, default=str)


def configure(service: str, level: str) -> None:
    """Install the JSON formatter as the root handler."""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter(service))

    root = logging.getLogger()
    root.handlers.clear()
    root.addHandler(handler)
    root.setLevel(level.upper())

    # uvicorn installs its own handlers; route them through ours so
    # access logs get the same redaction and trace IDs.
    for name in ("uvicorn", "uvicorn.access", "uvicorn.error"):
        uv = logging.getLogger(name)
        uv.handlers.clear()
        uv.propagate = True
