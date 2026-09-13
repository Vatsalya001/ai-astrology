"""Tests for PII redaction and trace propagation.

Mirrors services/api/internal/platform/logging/logging_test.go so both
languages carry the same guarantee, proven the same way.

A guard that has never been observed to fire is a guard you should not
trust — so every test here asserts the value is actually absent from the
output, not merely that the code ran.
"""

from __future__ import annotations

import json
import logging
from typing import Any

import pytest

from app.observability import (
    REDACTED,
    JSONFormatter,
    redact,
    set_trace_id,
)


def emit(service: str = "astro", **extra: Any) -> dict[str, Any]:
    """Format one log record and return it parsed."""
    record = logging.LogRecord(
        name="test",
        level=logging.INFO,
        pathname=__file__,
        lineno=1,
        msg="test event",
        args=(),
        exc_info=None,
    )
    for key, value in extra.items():
        setattr(record, key, value)

    return json.loads(JSONFormatter(service).format(record))


class TestRedaction:
    """Birth date, time and place are PII. In combination they are close
    to a unique identifier, which is exactly why they are on the list."""

    @pytest.mark.parametrize(
        ("key", "value"),
        [
            ("email", "someone@example.com"),
            ("phone", "+919876543210"),
            ("birth_date", "1994-08-17"),
            ("birth_time", "14:35"),
            ("birth_place", "Jaipur, Rajasthan"),
            ("latitude", "26.9124"),
            ("longitude", "75.7873"),
            ("password", "hunter2"),
            ("access_token", "eyJhbGciOi"),
            ("refresh_token", "r3fr35ht0k3n"),
            ("api_key", "sk-ant-secret"),
            ("internal_token", "dev-internal-token"),
            ("authorization", "Bearer abc123"),
            ("otp", "482913"),
            ("webhook_secret", "whsec_live"),
        ],
    )
    def test_sensitive_top_level_key_is_redacted(self, key: str, value: str) -> None:
        out = emit(**{key: value})
        assert out[key] == REDACTED
        assert value not in json.dumps(out)

    def test_redaction_is_case_insensitive(self) -> None:
        out = emit(Email="someone@example.com")
        assert "someone@example.com" not in json.dumps(out)

    def test_non_sensitive_fields_survive(self) -> None:
        out = emit(user_id="0f3a-uuid", status=200, path="/v1/charts/compute")
        assert out["user_id"] == "0f3a-uuid"
        assert out["status"] == 200
        assert out["path"] == "/v1/charts/compute"

    def test_name_cannot_be_supplied_at_top_level(self) -> None:
        """`name` is on the sensitive list but is unreachable via extra=.

        LogRecord.name is the *logger* name, and Python's logging module
        refuses to let `extra` shadow an existing record attribute. So a
        person's name can only arrive nested (covered below), and the
        formatter correctly treats top-level `name` as the logger.
        """
        logger = logging.getLogger("test.name.collision")
        # The record is only built when the level allows it; without this
        # the call short-circuits and nothing is raised.
        logger.setLevel(logging.INFO)

        with pytest.raises(KeyError, match="name"):
            logger.info("x", extra={"name": "A Person"})


class TestNestedRedaction:
    """Python can descend into structures; Go's slog cannot.

    This makes the Python side strictly stronger than the Go side. The
    'log IDs, not objects' discipline still applies — a redacted object
    is a large log line for no benefit — but a nested leak is caught.
    """

    def test_nested_dict_is_redacted(self) -> None:
        out = emit(profile={"id": "abc", "birth_place": "Jaipur", "email": "x@y.com"})
        assert out["profile"]["id"] == "abc"
        assert out["profile"]["birth_place"] == REDACTED
        assert out["profile"]["email"] == REDACTED
        assert "Jaipur" not in json.dumps(out)

    def test_nested_name_is_redacted(self) -> None:
        """The realistic path for a person's name reaching a log line."""
        out = emit(user={"id": "u-1", "name": "A Person"})
        assert out["user"]["id"] == "u-1"
        assert out["user"]["name"] == REDACTED
        assert "A Person" not in json.dumps(out)

    def test_list_of_dicts_is_redacted(self) -> None:
        out = emit(profiles=[{"phone": "+9198"}, {"phone": "+9199"}])
        assert all(p["phone"] == REDACTED for p in out["profiles"])
        assert "+9198" not in json.dumps(out)

    def test_deeply_nested_is_redacted(self) -> None:
        out = emit(a={"b": {"c": {"email": "deep@example.com"}}})
        assert "deep@example.com" not in json.dumps(out)

    def test_recursion_is_bounded(self) -> None:
        """A pathological structure must not hang the logger."""
        deep: dict[str, Any] = {}
        node = deep
        for _ in range(50):
            node["next"] = {}
            node = node["next"]
        node["email"] = "leak@example.com"

        result = json.dumps(redact(deep))
        assert "[TRUNCATED]" in result
        # Truncation happens well before the leak could be reached.
        assert "leak@example.com" not in result

    def test_cyclic_structure_does_not_hang(self) -> None:
        cyclic: dict[str, Any] = {"name": "x"}
        cyclic["self"] = cyclic
        redact(cyclic)  # must return rather than recurse forever


class TestTraceID:
    def test_trace_id_is_attached(self) -> None:
        set_trace_id("trace-abc-123")
        try:
            assert emit()["trace_id"] == "trace-abc-123"
        finally:
            set_trace_id("")

    def test_absent_when_unset(self) -> None:
        set_trace_id("")
        assert "trace_id" not in emit()


class TestFormat:
    def test_shape_matches_go_service(self) -> None:
        """Field names must match services/api so logs from all three
        services can be queried with one set of filters."""
        out = emit(service="astro")
        for field in ("time", "level", "service", "msg"):
            assert field in out, f"missing field {field}"
        assert out["service"] == "astro"
        assert out["level"] == "INFO"

    def test_output_is_one_json_object_per_line(self) -> None:
        record = logging.LogRecord("t", logging.INFO, __file__, 1, "multi\nline", (), None)
        formatted = JSONFormatter("astro").format(record)
        assert "\n" not in formatted
        json.loads(formatted)
