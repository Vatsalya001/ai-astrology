"""astro-service — deterministic Vedic astrology computation.

This service turns birth data into a chart. It is:

  * stateless  — no database, no cache, no session
  * pure       — same input always produces the same output
  * offline    — no outbound calls except optional telemetry export
  * AI-free    — structurally incapable of calling a language model

That last property is the point. An LLM must never compute a planetary
position, and the most reliable way to guarantee that is to put the
computation somewhere a model cannot be reached from.

Phase 2 fills in app/core/ with the actual ephemeris work.
"""

import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app import middleware, telemetry
from app.api import health
from app.env_check import assert_no_typos
from app.observability import configure as configure_logging
from app.settings import settings

__version__ = "0.1.0"


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    configure_logging(service="astro", level=settings.log_level)
    log = logging.getLogger("astro")

    # A misspelled env var is silently ignored by pydantic-settings,
    # leaving the setting at its default. For DEFAULT_AYANAMSA that
    # would mean every chart computed against the wrong zodiac.
    assert_no_typos(settings)
    telemetry.init(app, "astro", __version__, settings.env)

    log.info(
        "starting astro-service",
        extra={
            "env": settings.env,
            "ayanamsa": settings.default_ayanamsa,
            "house_system": settings.default_house_system,
            "ephemeris": settings.ephemeris_flag,
        },
    )
    # Phase 2 loads the Swiss Ephemeris here, once, rather than per request.
    yield
    log.info("astro-service shutdown complete")


app = FastAPI(
    title="astro-service",
    version=__version__,
    description="Deterministic Vedic astrology computation. Internal only.",
    lifespan=lifespan,
    # No docs in production: this service is internal, and its schema is
    # a map of the system for anyone who reaches it.
    docs_url=None if settings.is_production else "/docs",
    redoc_url=None,
    openapi_url="/openapi.json",
)

middleware.install(app, internal_token=settings.internal_token)
app.include_router(health.router)
