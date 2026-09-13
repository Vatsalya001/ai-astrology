"""Health endpoint.

The response shape is identical across all three services so that
api-service's aggregate health check needs no per-service special casing.
"""

from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter(tags=["ops"])


class HealthResponse(BaseModel):
    status: str
    service: str
    version: str


@router.get("/health", response_model=HealthResponse)
async def health() -> HealthResponse:
    """Liveness check.

    astro-service has no dependencies to probe — no database, no cache,
    no upstream services. If the process is answering, it is healthy.
    That is a direct consequence of being stateless, and it is why this
    service can be scaled by simply adding replicas.
    """
    from app.main import __version__

    return HealthResponse(status="ok", service="astro", version=__version__)
