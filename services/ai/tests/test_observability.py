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


def emit(service: str = "ai", **extra: Any) -> dict[str, Any]:
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
        out = emit(service="ai")
        for field in ("time", "level", "service", "msg"):
            assert field in out, f"missing field {field}"
        assert out["service"] == "ai"
        assert out["level"] == "INFO"

    def test_output_is_one_json_object_per_line(self) -> None:
        record = logging.LogRecord("t", logging.INFO, __file__, 1, "multi\nline", (), None)
        formatted = JSONFormatter("ai").format(record)
        assert "\n" not in formatted
        json.loads(formatted)


# ─── §14: the key must be absent from every Sentry payload ───────────


class TestTheSentryScrubber:
    """`_scrub_event` was untested and covered only the REQUEST.

    It strips sensitive headers, the query string, the body and cookies
    — everything Sentry collects from the request. It did not touch the
    place a provider key actually leaks: an exception MESSAGE.

    Every adapter builds its own error from a status code so the key
    cannot reach it, but each one does `raise _classify(err) from err`,
    and Sentry serialises the whole `__cause__` chain. The vendor SDK's
    own exception is in that chain, and an auth failure can name the
    credential that failed.
    """

    KEY = "sk-ant-api03-" + "Z" * 32

    def _event_with_key_in_an_exception(self) -> dict:
        return {
            "exception": {
                "values": [
                    {
                        "type": "AuthenticationError",
                        "value": f"401 invalid x-api-key: {self.KEY}",
                        "stacktrace": {"frames": [{"vars": {"api_key": self.KEY}}]},
                    }
                ]
            },
            "breadcrumbs": {"values": [{"message": f"calling anthropic with {self.KEY}"}]},
            "extra": {"provider_config": {"key": self.KEY}},
            "request": {"headers": {"X-Internal-Token": "tok", "Accept": "application/json"}},
        }

    def test_the_key_is_scrubbed_from_an_exception_message(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        from app import telemetry
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_api_key", self.KEY)

        scrubbed = telemetry._scrub_event(self._event_with_key_in_an_exception(), {})

        assert scrubbed is not None
        serialised = json.dumps(scrubbed)
        assert self.KEY not in serialised, "the provider key survived into the Sentry payload"
        assert "[REDACTED]" in serialised

    def test_it_reaches_breadcrumbs_extra_and_stack_vars(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Named separately because each is a different nesting shape.

        A scrubber that walked only `exception` would pass the test
        above and leave the key in three other places Sentry renders.
        """
        from app import telemetry
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_api_key", self.KEY)

        scrubbed = telemetry._scrub_event(self._event_with_key_in_an_exception(), {})
        assert scrubbed is not None

        assert self.KEY not in json.dumps(scrubbed.get("breadcrumbs"))
        assert self.KEY not in json.dumps(scrubbed.get("extra"))
        assert self.KEY not in json.dumps(scrubbed.get("exception"))

    @pytest.mark.parametrize(
        "setting_name",
        ["llm_api_key", "llm_fallback_api_key", "internal_token"],
    )
    def test_every_configured_credential_is_scrubbed(
        self, monkeypatch: pytest.MonkeyPatch, setting_name: str
    ) -> None:
        """Parametrised over ALL of them, because one was missing.

        `llm_fallback_api_key` was absent from `_secret_values()`, and
        it is the credential this matters most for: a fallback exists in
        order to be called when the primary is failing, which is exactly
        when Sentry is collecting events. A vendor 401 from the fallback
        carried its key verbatim into the report — in the same string
        where the primary's had just been redacted, which is what makes
        the old tests' green so misleading.

        Parametrised rather than written out three times so that adding
        a credential to Settings without adding it here is a failing
        test rather than something an audit has to find.
        """
        from app import telemetry
        from app.settings import settings

        secret = "zz-" + setting_name + "-" + "Q" * 32

        # Every credential set to something, so the test cannot pass
        # merely because the scrubber found SOME secret in the string.
        monkeypatch.setattr(settings, "llm_api_key", "sk-primary-" + "A" * 32)
        monkeypatch.setattr(settings, "llm_fallback_api_key", "AIza-fallback-" + "B" * 32)
        monkeypatch.setattr(settings, "internal_token", "internal-" + "C" * 32)
        monkeypatch.setattr(settings, setting_name, secret)

        event = {
            "exception": {
                "values": [{"type": "ClientError", "value": f"401 key not valid: {secret}"}]
            },
            "breadcrumbs": {"values": [{"message": f"calling vendor with {secret}"}]},
            "extra": {"provider_config": {"key": secret}},
        }

        scrubbed = telemetry._scrub_event(event, {})

        assert scrubbed is not None
        assert secret not in json.dumps(scrubbed), (
            f"{setting_name} survived into the Sentry payload"
        )

    def test_the_internal_token_is_scrubbed_too(self, monkeypatch: pytest.MonkeyPatch) -> None:
        from app import telemetry
        from app.settings import settings

        token = "internal-token-that-is-long-enough"
        monkeypatch.setattr(settings, "internal_token", token)

        scrubbed = telemetry._scrub_event({"extra": {"note": f"sent {token}"}}, {})

        assert scrubbed is not None
        assert token not in json.dumps(scrubbed)

    def test_ordinary_content_is_left_alone(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """The negative case.

        A scrubber that redacted everything would pass every test above
        and make error reporting useless — which is worse than the leak,
        because nobody would notice it stopped working.
        """
        from app import telemetry
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_api_key", self.KEY)

        event = {"exception": {"values": [{"value": "connection refused to localhost:11434"}]}}
        scrubbed = telemetry._scrub_event(event, {})

        assert scrubbed is not None
        assert "connection refused to localhost:11434" in json.dumps(scrubbed)

    def test_a_short_secret_is_not_used_for_redaction(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """A two-character "secret" would match inside ordinary words.

        The dev default for a key is often "" or a placeholder, and
        redacting on one would turn every event into redaction soup —
        a scrubber nobody can read is a scrubber that gets disabled.
        """
        from app import telemetry
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_api_key", "ab")

        scrubbed = telemetry._scrub_event({"extra": {"note": "a fabulous absolute"}}, {})

        assert scrubbed is not None
        assert "fabulous" in json.dumps(scrubbed)

    def test_a_genuinely_cyclic_structure_does_not_recurse_forever(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Error reporting must not become the outage.

        The first version of this test built a 60-deep TREE and called
        it cyclic. Python handles 60 frames without complaint, so it
        passed with the depth bound removed — a test named for a hazard
        it did not construct.

        A dict containing itself is the real thing: without the bound
        `_redact_secrets` follows the cycle until the interpreter stops
        it, inside a `before_send` hook, while the process is already
        handling an error.
        """
        from app import telemetry
        from app.settings import settings

        monkeypatch.setattr(settings, "llm_api_key", self.KEY)

        cycle: dict = {"note": f"key is {self.KEY}"}
        cycle["self"] = cycle
        event = {"extra": cycle}

        scrubbed = telemetry._scrub_event(event, {})

        assert scrubbed is not None
        # The top level is still scrubbed — the bound limits how deep it
        # follows the cycle, it does not skip the work.
        assert self.KEY not in str(scrubbed["extra"]["note"])
