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
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from fastapi import FastAPI

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


def _scrub_event(event: dict[str, Any], _hint: dict[str, Any]) -> dict[str, Any] | None:
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

    # The user object is an ID and nothing else.
    user = event.get("user")
    if isinstance(user, dict):
        event["user"] = {"id": user.get("id")} if user.get("id") else {}

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
