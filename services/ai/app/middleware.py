"""HTTP middleware shared by both Python services.

NOTE: duplicated verbatim in services/astro — see the note in
app/observability.py for why.
"""

from __future__ import annotations

import logging
import time
import uuid

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse
from starlette.middleware.base import BaseHTTPMiddleware, RequestResponseEndpoint
from starlette.types import ASGIApp

from app.observability import set_trace_id

TRACE_HEADER = "X-Trace-Id"
INTERNAL_TOKEN_HEADER = "X-Internal-Token"

# Paths reachable without the internal token. /health must stay open so
# api-service can probe this service's liveness without holding a
# credential, and so container orchestrators can too.
PUBLIC_PATHS = frozenset({"/health", "/openapi.json", "/docs", "/redoc"})

log = logging.getLogger(__name__)


class TraceMiddleware(BaseHTTPMiddleware):
    """Adopt the caller's trace ID, or mint one.

    Honouring an inbound ID is what makes a single user request
    correlatable across web -> Go -> Python. The length cap matters
    because this is attacker-controlled input that ends up in log lines.
    """

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        incoming = request.headers.get(TRACE_HEADER, "")
        trace = incoming if 0 < len(incoming) <= 64 else str(uuid.uuid4())

        set_trace_id(trace)
        request.state.trace_id = trace

        start = time.perf_counter()
        response = await call_next(request)
        duration_ms = int((time.perf_counter() - start) * 1000)

        response.headers[TRACE_HEADER] = trace

        # Note what is absent: no query string, no body, no headers.
        # Any of those can carry PII in this product.
        log.info(
            "http request",
            extra={
                "method": request.method,
                "path": request.url.path,
                "status": response.status_code,
                "duration_ms": duration_ms,
            },
        )
        return response


class InternalTokenMiddleware(BaseHTTPMiddleware):
    """Reject calls that lack the shared service-to-service secret.

    This service is not reachable from the internet by design. The token
    is defence in depth for the case where that network boundary is
    misconfigured.
    """

    def __init__(self, app: ASGIApp, token: str) -> None:
        super().__init__(app)
        self._token = token

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        if request.url.path in PUBLIC_PATHS:
            return await call_next(request)

        supplied = request.headers.get(INTERNAL_TOKEN_HEADER, "")

        # Constant-time comparison: a naive == leaks the shared secret
        # one byte at a time to anyone who can measure response latency.
        if not _constant_time_equals(supplied, self._token):
            log.warning(
                "rejected unauthenticated internal call",
                extra={"path": request.url.path},
            )
            # Generic message. Never confirm whether the token was
            # absent, malformed or merely wrong.
            return JSONResponse(
                status_code=401,
                content={"error": {"code": "UNAUTHORIZED", "message": "Unauthorized."}},
            )

        return await call_next(request)


def _constant_time_equals(a: str, b: str) -> bool:
    import hmac

    return hmac.compare_digest(a.encode("utf-8"), b.encode("utf-8"))


def install(app: FastAPI, internal_token: str) -> None:
    """Install middleware in the correct order.

    Starlette runs middleware in reverse registration order, so
    registering Trace last means it runs first — and therefore the
    rejection log line from InternalTokenMiddleware still carries a
    trace ID.
    """
    app.add_middleware(InternalTokenMiddleware, token=internal_token)
    app.add_middleware(TraceMiddleware)
