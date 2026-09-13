"""Configuration for ai-service."""

from typing import Literal

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

# A provider's tier determines whether it may be used in production.
#   local       — runs on this machine, nothing leaves it (Ollama)
#   free-hosted — a free API tier; may train on inputs
#   paid        — commercial tier with a no-training commitment
ProviderTier = Literal["local", "free-hosted", "paid"]


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="forbid",
        case_sensitive=False,
    )

    env: Literal["development", "staging", "production"] = "development"
    ai_port: int = 8200
    log_level: Literal["debug", "info", "warning", "error"] = "info"

    internal_token: str = "dev-internal-token-change-me"

    # ─── Database ─────────────────────────────────────────────────
    # A READ-ONLY role. ai-service never writes; api-service is the sole
    # writer (ADR-001). This is enforced by Postgres grants, not by
    # convention — see infrastructure/docker/init/01-init.sql.
    ai_database_url: str = "postgresql://astro_ro:astro_ro@localhost:5432/astro_dev"

    # ─── LLM provider ─────────────────────────────────────────────
    llm_provider: Literal["openai-compatible", "anthropic", "google", "mock"] = "openai-compatible"
    llm_base_url: str = "http://localhost:11434/v1"
    llm_api_key: str = ""
    llm_provider_tier: ProviderTier = "local"

    llm_model_fast: str = "llama3.2:3b"
    llm_model_chat: str = "qwen2.5:7b"
    llm_model_deep: str = "qwen2.5:7b"

    # ─── Embeddings ───────────────────────────────────────────────
    embedding_provider: str = "ollama"
    embedding_model: str = "nomic-embed-text"
    # Never hardcode this anywhere else. nomic-embed-text is 768; most
    # hosted models are 1024. A pgvector column is fixed-dimension, so a
    # literal in a migration means re-embedding the whole corpus later.
    embedding_dim: int = Field(default=768, ge=64, le=4096)

    @property
    def is_production(self) -> bool:
        return self.env == "production"


settings = Settings()
