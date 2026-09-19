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
    ModelMap,
    ModelTier,
    ProviderError,
    ProviderTier,
    RequestMetadata,
    Role,
    SystemBlock,
    Usage,
)
from app.providers.mock import MockProvider, request_fingerprint
from app.providers.openai_compatible import OpenAICompatibleProvider
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
    "MockProvider",
    "ModelMap",
    "ModelTier",
    "NoProviderAvailableError",
    "OpenAICompatibleProvider",
    "ProviderError",
    "ProviderRegistry",
    "ProviderTier",
    "RequestMetadata",
    "Role",
    "SystemBlock",
    "Usage",
    "request_fingerprint",
]
