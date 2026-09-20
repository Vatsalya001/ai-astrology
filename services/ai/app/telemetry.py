"""Optional OpenTelemetry tracing and Sentry error reporting.

Both are disabled unless configured. Development and CI run without a
collector or a Sentry project, and requiring either would make the stack
harder to start for no benefit.

What is NOT optional is the W3C trace-context propagator: it is what
carries a trace from api-service into this one, and it costs nothing.

NOTE: duplicated verbatim in the sibling service — see app/observability.py
for why the two keep their own copies.
"""

from __future__ import annotations

import logging
import os
from typing import TYPE_CHECKING, cast

if TYPE_CHECKING:
    from fastapi import FastAPI

    # sentry_sdk is an optional extra, so these are type-only imports.
    # CI installs --all-extras and therefore type-checks against the real
    # signatures; a local checkout without the extra falls back to Any
    # via the mypy override in pyproject.toml. CI is the stricter of the
    # two, which is the right way round.
    from sentry_sdk.types import Event, Hint

log = logging.getLogger(__name__)

# Request headers that must never reach an error reporter. Sentry
# captures request context automatically, and this system's requests
# carry the internal service credential.
_SENSITIVE_HEADERS = frozenset(
    {
        "authorization",
        "cookie",
        "set-cookie",
        "x-internal-token",
        "idempotency-key",
        "proxy-authorization",
    }
)


# Below this length a "secret" is too short to redact safely: a
# two-character value would match inside ordinary words and turn every
# event into redaction soup. Real keys are far longer; a placeholder
# like "" or "x" is not worth protecting.
_MIN_REDACTABLE_SECRET = 12

# Bound on recursion. A cyclic or pathological structure must not turn
# error reporting into a hang — the same reasoning as the depth cap in
# app/observability.py.
_MAX_SCRUB_DEPTH = 12


def _secret_values() -> tuple[str, ...]:
    """The configured secrets, as VALUES to hunt for.

    `_SENSITIVE_HEADERS` scrubs by header NAME, which covers what Sentry
    collects from the request. It does not cover where a provider key
    actually leaks: inside an exception MESSAGE.

    The adapters build their own errors from a status code so the key
    cannot reach them — but every one of them does `raise _classify(err)
    from err`, and Sentry serialises the whole `__cause__` chain. The
    SDK's own exception is in that chain, and an auth failure from a
    vendor can name the credential that failed.

    So the last line of defence is a value scan. It costs one string
    walk per reported event, which only happens when something already
    went wrong.
    """
    from app.settings import settings

    return tuple(
        value
        for value in (settings.llm_api_key, settings.internal_token)
        if value and len(value) >= _MIN_REDACTABLE_SECRET
    )


def _redact_secrets(value: object, secrets: tuple[str, ...], depth: int = 0) -> object:
    """Replace any occurrence of a known secret, anywhere in the event.

    Walks rather than pattern-matches: a regex for "things that look
    like an API key" fails on the vendor whose format it does not know,
    and this service talks to four vendors. The configured value is the
    one thing that is certainly a secret.
    """
    if depth > _MAX_SCRUB_DEPTH:
        return value

    if isinstance(value, str):
        for secret in secrets:
            value = value.replace(secret, "[REDACTED]")
        return value
    if isinstance(value, dict):
        return {k: _redact_secrets(v, secrets, depth + 1) for k, v in value.items()}
    if isinstance(value, list):
        return [_redact_secrets(v, secrets, depth + 1) for v in value]
    if isinstance(value, tuple):
        return tuple(_redact_secrets(v, secrets, depth + 1) for v in value)
    return value


def _scrub_event(event: Event, _hint: Hint) -> Event | None:
    """Strip credentials and PII before an event leaves the process."""
    request = event.get("request")
    if isinstance(request, dict):
        headers = request.get("headers")
        if isinstance(headers, dict):
            for name in list(headers):
                if name.lower() in _SENSITIVE_HEADERS:
                    headers.pop(name, None)
        # A query string can carry an OTP or an email from a badly-built
        # link; a body can carry birth details. Drop both wholesale
        # rather than trying to identify the safe parts.
        request.pop("query_string", None)
        request.pop("data", None)
        request.pop("cookies", None)

    # The user object is an ID and nothing else. Sentry's UI will happily
    # display an email if it is given one.
    user = event.get("user")
    if isinstance(user, dict):
        user_id = user.get("id")
        event["user"] = {"id": user_id} if user_id else {}

    # Last, and over everything: exception messages, breadcrumbs, extra,
    # contexts. §14 asks for the key to be absent from every Sentry
    # payload, and the structural scrubbing above only covers what
    # Sentry collected from the REQUEST.
    secrets = _secret_values()
    if secrets:
        scrubbed = _redact_secrets(dict(event), secrets)
        if isinstance(scrubbed, dict):
            return cast("Event", scrubbed)

    return event


def init(app: FastAPI, service: str, version: str, env: str) -> None:
    """Wire tracing and error reporting. Safe to call unconditionally."""
    _init_tracing(app, service, version, env)
    _init_sentry(service, version, env)


def _init_tracing(app: FastAPI, service: str, version: str, env: str) -> None:
    endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", "")

    try:
        from opentelemetry import trace
        from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import BatchSpanProcessor
    except ImportError:
        # The OTel packages are optional extras. Their absence is not an
        # error — it just means no tracing.
        log.debug("opentelemetry not installed; tracing disabled")
        return

    provider = TracerProvider(
        resource=Resource.create(
            {
                "service.name": service,
                "service.version": version,
                "deployment.environment": env,
            }
        )
    )

    if endpoint:
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter,
        )

        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
        log.info("tracing enabled", extra={"otlp_endpoint": endpoint})

    # The provider is installed even with no exporter, so instrumentation
    # code paths still execute and cannot rot between releases.
    trace.set_tracer_provider(provider)

    # excluded_urls keeps health-check noise out of the trace store.
    # Orchestrators poll /health every few seconds forever.
    FastAPIInstrumentor.instrument_app(app, excluded_urls="health,ready")


def _init_sentry(service: str, version: str, env: str) -> None:
    dsn = os.environ.get("SENTRY_DSN", "")
    if not dsn:
        log.debug("sentry disabled: no DSN configured")
        return

    try:
        import sentry_sdk
    except ImportError:
        log.warning("SENTRY_DSN is set but sentry-sdk is not installed")
        return

    sentry_sdk.init(
        dsn=dsn,
        environment=env,
        release=f"{service}@{version}",
        # Never send request bodies or user identifiers by default.
        send_default_pii=False,
        # Tracing goes to OpenTelemetry. One tracing backend.
        traces_sample_rate=0.0,
        before_send=_scrub_event,
    )
    log.info("error reporting enabled")
