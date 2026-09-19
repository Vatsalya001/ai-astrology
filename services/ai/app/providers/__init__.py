"""Every model this product talks to.

The vendor SDKs live here and only here. `.importlinter` enforces that
in CI — see the `providers-are-the-only-vendor-boundary` contract — so
an orchestrator cannot quietly `from openai import ...` and turn a
configuration change back into a code change.
"""

from app.providers.base import (
    Capabilities,
    CompletionChunk,
    CompletionRequest,
    CompletionResponse,
    EmbeddingProvider,
    FinishReason,
    LLMProvider,
    Message,
    ModelTier,
    ProviderError,
    ProviderTier,
    RequestMetadata,
    Role,
    SystemBlock,
    Usage,
)
from app.providers.registry import NoProviderAvailableError, ProviderRegistry

__all__ = [
    "Capabilities",
    "CompletionChunk",
    "CompletionRequest",
    "CompletionResponse",
    "EmbeddingProvider",
    "FinishReason",
    "LLMProvider",
    "Message",
    "ModelTier",
    "NoProviderAvailableError",
    "ProviderError",
    "ProviderRegistry",
    "ProviderTier",
    "RequestMetadata",
    "Role",
    "SystemBlock",
    "Usage",
]
