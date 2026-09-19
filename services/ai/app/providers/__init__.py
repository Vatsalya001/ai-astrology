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
from app.providers.ollama_embeddings import OllamaEmbeddingProvider
from app.providers.openai_compatible import OpenAICompatibleProvider
from app.providers.registry import NoProviderAvailableError, ProviderRegistry
from app.providers.resilience import BreakerState, CircuitOpenError, ResilientProvider

__all__ = [
    "BreakerState",
    "Capabilities",
    "CircuitOpenError",
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
    "OllamaEmbeddingProvider",
    "OpenAICompatibleProvider",
    "ProviderError",
    "ProviderRegistry",
    "ProviderTier",
    "RequestMetadata",
    "ResilientProvider",
    "Role",
    "SystemBlock",
    "Usage",
    "request_fingerprint",
]
