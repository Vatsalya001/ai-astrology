"""ai-service — LLM orchestration, RAG, safety and evaluation.

Everything probabilistic lives here. Everything deterministic lives in
astro-service. The wall between them is a service boundary rather than a
code convention, which is what makes the determinism principle hold
under pressure.

This service:
  * reads from Postgres with a READ-ONLY role (api-service is the sole writer)
  * talks to a model provider chosen entirely by configuration
  * refuses to start on a non-paid provider in production (app/guards.py)

Phase 4 fills in app/providers/, app/prompts/ and app/safety/.
Phase 5 adds retrieval. Phase 6 adds the eval harness.
"""

import logging
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app import middleware
from app.api import health
from app.guards import run_all_startup_guards
from app.observability import configure as configure_logging
from app.settings import settings

__version__ = "0.1.0"


@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    configure_logging(service="ai", level=settings.log_level)
    log = logging.getLogger("ai")

    # Guards run before anything else binds or connects. A configuration
    # that must never serve traffic should never reach the point of
    # being able to.
    run_all_startup_guards()

    log.info(
        "starting ai-service",
        extra={
            "env": settings.env,
            "provider": settings.llm_provider,
            "provider_tier": settings.llm_provider_tier,
            "model_fast": settings.llm_model_fast,
            "model_chat": settings.llm_model_chat,
            "model_deep": settings.llm_model_deep,
        },
    )

    if settings.llm_provider_tier != "paid":
        log.info(
            "running on a non-paid provider — development only. "
            "No real user data may reach this provider.",
            extra={"provider_tier": settings.llm_provider_tier},
        )

    yield
    log.info("ai-service shutdown complete")


app = FastAPI(
    title="ai-service",
    version=__version__,
    description="LLM orchestration, RAG and safety. Internal only.",
    lifespan=lifespan,
    docs_url=None if settings.is_production else "/docs",
    redoc_url=None,
    openapi_url="/openapi.json",
)

middleware.install(app, internal_token=settings.internal_token)
app.include_router(health.router)
