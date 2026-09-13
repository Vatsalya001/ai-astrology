"""astro-service — deterministic Vedic astrology computation.

This service turns birth data into a chart. It is:

  * stateless  — no database, no cache, no session
  * pure       — same input always produces the same output
  * offline    — no outbound network calls of any kind
  * AI-free    — structurally incapable of calling a language model

That last property is the point. An LLM must never compute a planetary
position, and the most reliable way to guarantee that is to put the
computation somewhere a model cannot be reached from.

Phase 2 fills in app/core/ with the actual ephemeris work.
"""

import logging
import sys
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api import health
from app.settings import settings

__version__ = "0.1.0"


def _configure_logging() -> None:
    """Structured logging to stdout, matching the Go service's shape."""
    logging.basicConfig(
        level=settings.log_level.upper(),
        stream=sys.stdout,
        format='{"ts":"%(asctime)s","level":"%(levelname)s","service":"astro","msg":"%(message)s"}',
    )


@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    _configure_logging()
    log = logging.getLogger("astro")
    log.info(
        "starting astro-service env=%s ayanamsa=%s houses=%s ephemeris=%s",
        settings.env,
        settings.default_ayanamsa,
        settings.default_house_system,
        settings.ephemeris_flag,
    )
    # Phase 2 loads the Swiss Ephemeris here, once, rather than per
    # request.
    yield
    log.info("astro-service shutdown complete")


app = FastAPI(
    title="astro-service",
    version=__version__,
    description="Deterministic Vedic astrology computation. Internal only.",
    lifespan=lifespan,
    # No docs in production: this service is internal, and its schema
    # is a map of the system for anyone who reaches it.
    docs_url=None if settings.is_production else "/docs",
    redoc_url=None,
    openapi_url="/openapi.json",
)

app.include_router(health.router)
