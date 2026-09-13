"""ai-service — LLM orchestration, RAG, safety and evaluation.

Everything probabilistic lives here. Everything deterministic lives in
astro-service. The wall between them is a service boundary rather than a
code convention, which is what makes the determinism principle hold under
pressure.

This service:
  * reads from Postgres with a READ-ONLY role (api-service is the sole writer)
  * talks to a model provider chosen entirely by configuration
  * refuses to start on a non-paid provider in production (see app/guards.py)

Phase 4 fills in app/providers/, app/prompts/ and app/safety/.
Phase 5 adds retrieval. Phase 6 adds the eval harness.
"""

import logging
import sys
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api import health
from app.guards import run_all_startup_guards
from app.settings import settings

__version__ = "0.1.0"


def _configure_logging() -> None:
    logging.basicConfig(
        level=settings.log_level.upper(),
        stream=sys.stdout,
        format='{"ts":"%(asctime)s","level":"%(levelname)s","service":"ai","msg":"%(message)s"}',
    )


@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    _configure_logging()
    log = logging.getLogger("ai")

    # Guards run before anything else binds or connects. A configuration
    # that must never serve traffic should never reach the point of
    # being able to.
    run_all_startup_guards()

    log.info(
        "starting ai-service env=%s provider=%s tier=%s models=[fast=%s chat=%s deep=%s]",
        settings.env,
        settings.llm_provider,
        settings.llm_provider_tier,
        settings.llm_model_fast,
        settings.llm_model_chat,
        settings.llm_model_deep,
    )

    if settings.llm_provider_tier != "paid":
        log.info(
            "running on a %s provider — development only. "
            "No real user data may reach this provider.",
            settings.llm_provider_tier,
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

app.include_router(health.router)
