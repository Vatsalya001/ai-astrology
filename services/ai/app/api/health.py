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

    providers: dict[str, bool] | None = None
    """Per-provider reachability, present only when `?probe=true`.

    `None` — the field is absent — when nothing was probed, which is
    deliberately different from `{}`. An empty object would read as "we
    looked and found no providers", which is a real and different
    failure.
    """


@router.get("/health", response_model=HealthResponse)
async def health(probe: bool = False) -> HealthResponse:
    """Liveness by default; provider reachability on request.

    ── Why the default does not probe ──

    Provider reachability is a runtime concern handled by the retry and
    circuit-breaker logic, not a reason to mark this process unhealthy
    and have it restarted. A chain whose primary is down but whose
    fallback answers is still serving users, and reporting it unhealthy
    would pull a working service out of a load balancer.

    That half of the original reasoning stands. The other half said a
    probe "calls an LLM and costs money on every poll" — which is not
    what the probe does: `health_check()` calls `models.list()`, which
    spends no tokens and needs no model pulled. The cost is a round trip,
    not a bill.

    ── Why it is offered at all ──

    Because the gap it left was real and was observed: with Ollama
    unreachable, `api-service` reported `"ai": {"status": "ok"}` while
    every `/v1/complete` failed. Nothing anywhere said which provider
    was unreachable, and `ProviderRegistry.health()` — whose docstring
    says it is "for the health endpoint" — had no caller outside a test.

    Opt-in rather than always-on so routine liveness polls stay free of
    a network round trip, and so this can never be the reason a pod is
    restarted: `status` is unaffected by what the probe finds.
    """
    from app.main import __version__

    providers: dict[str, bool] | None = None
    if probe:
        # Imported here, not at module scope: importing the chain builds
        # providers, and a liveness endpoint must not depend on provider
        # construction succeeding.
        from app.api.complete import get_provider_chain

        try:
            providers = await get_provider_chain().health()
        except Exception:
            # A probe that raises must not take down the liveness
            # answer. An empty map is the honest report: we tried and
            # learned nothing.
            providers = {}

    return HealthResponse(
        status="ok",
        service="ai",
        version=__version__,
        provider=settings.llm_provider,
        provider_tier=settings.llm_provider_tier,
        providers=providers,
    )
