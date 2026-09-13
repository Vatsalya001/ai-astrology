"""Health endpoint for ai-service.

Shape matches astro-service and api-service so the aggregate check in Go
needs no per-service special casing.
"""

from fastapi import APIRouter
from pydantic import BaseModel

from app.settings import settings

router = APIRouter(tags=["ops"])


class HealthResponse(BaseModel):
    status: str
    service: str
    version: str
    provider: str
    provider_tier: str


@router.get("/health", response_model=HealthResponse)
async def health() -> HealthResponse:
    """Liveness check.

    Deliberately does NOT probe the model provider. Two reasons:

      * A health check that calls an LLM costs money on every poll and
        adds seconds of latency to something that should take
        milliseconds.
      * Provider reachability is a runtime concern handled by the retry
        and circuit-breaker logic in Phase 4, not a reason to mark this
        process unhealthy and have it restarted.

    The provider fields are reported so operators can see at a glance
    which backend is configured — useful when the answer to "why is dev
    output worse than prod?" is "dev is on a 3B local model".
    """
    from app.main import __version__

    return HealthResponse(
        status="ok",
        service="ai",
        version=__version__,
        provider=settings.llm_provider,
        provider_tier=settings.llm_provider_tier,
    )
