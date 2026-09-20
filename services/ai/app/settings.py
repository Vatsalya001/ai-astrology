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

    # ─── Fallback provider (PHASE-04 §2) ──────────────────────────
    # Empty by default, and that is the point: a chain that grows a
    # second link nobody configured would silently move traffic to a
    # provider whose tier, price and data-retention terms were never
    # reviewed — on the one day the primary is down and nobody is
    # reading logs.
    #
    # `openai-compatible` is deliberately not offerable here. §11 defines
    # no second base URL, so such a fallback would point at the endpoint
    # the primary just failed at, and would collide with it on provider
    # id — a "fallback" that is the primary wearing a different name.
    llm_fallback_provider: Literal["", "anthropic", "google", "mock"] = ""
    llm_fallback_api_key: str = ""

    # ─── Resilience (PHASE-04 §2, task 4.9) ───────────────────────
    # One operator-visible knob per behaviour. The adapters carry their
    # own defaults (60s, 120s) and `ResilientProvider` carries its own
    # budgets; a default that configuration cannot reach is a knob that
    # lies, so app/api/complete.py passes every one of these in.
    llm_timeout_seconds: float = Field(default=60.0, gt=0)
    """Per-provider HTTP budget. §11's documented default is 60.

    It is a FLOOR for the paid provider rather than an absolute: see
    `effective_llm_timeout_seconds`. Wiring this straight through took
    AnthropicProvider from its own 120s default down to 60s, and the
    deep tier — a paid interpretation on the largest model — is exactly
    the call that needs the longer budget. A regression that only shows
    up on the most expensive request in the product is the worst kind to
    ship silently.
    """

    llm_paid_timeout_floor_seconds: float = Field(default=120.0, gt=0)
    """The floor. Matches AnthropicProvider's own former default.

    Applied only when the provider tier is `paid`: a local model that
    has not answered in 60s is not going to, and waiting two minutes for
    it would make a dev machine feel broken.
    """

    # RETRIES, not attempts: 2 here means three calls in total. The
    # off-by-one is wired once, in complete.py, so the documented number
    # means what an operator reading §11 expects it to mean.
    llm_max_retries: int = Field(default=2, ge=0, le=10)

    llm_circuit_breaker_threshold: int = Field(default=5, ge=1)
    llm_circuit_breaker_reset_seconds: float = Field(default=60.0, gt=0)

    # ─── Prompt versions (PHASE-04 §5) ────────────────────────────
    # A published prompt version is immutable, so selecting one is
    # configuration rather than a code change. The pattern rejects a
    # malformed name at startup — `latest`, `1`, `v1 ` — which would
    # otherwise reach app/prompts/registry.py as a filename and fail on
    # the first user's request. A well-formed version that was never
    # published still fails there; this only moves the obvious half of
    # the mistake to boot time.
    #
    # `intent` defaults to v2, not to §11's illustrative `v1`. v1 is
    # frozen and still loadable, but it scored 0% on the labelled set
    # because it did not name its output fields (see
    # app/classification/classifier.py) — defaulting to it would demote
    # the classifier to useless as a side effect of declaring a setting.
    prompt_version_chat: str = Field(default="v1", pattern=r"^v[0-9]+$")
    prompt_version_intent: str = Field(default="v2", pattern=r"^v[0-9]+$")

    # ─── Persona and safety (PHASE-04 §5, §7) ─────────────────────
    # These three are declared so the documented .env boots, and each is
    # narrowed to the only value this phase can honour. The alternative —
    # accepting any value and ignoring it — is the failure mode
    # app/env_check.py exists to prevent: believing you configured
    # something while the service runs on a default.
    #
    # Honouring another persona needs app/orchestrator/pipeline.py, where
    # PERSONA_FOR maps intent → persona and DEFAULT_PERSONA is the
    # fallback for intents it does not name.
    default_persona: Literal["vedic_guide"] = "vedic_guide"

    # `false` is refused at startup by app/guards.py rather than here, so
    # the error can say WHY the switch does not exist. Layer 3 of §7's
    # safety design (the output validator) is not an optional feature:
    # turning it off means a fabricated planetary placement reaches a
    # user, which is the one thing that layer exists to stop.
    safety_validation_enabled: bool = True
    safety_block_on_fabricated_fact: bool = True

    # India only, and narrowed on purpose. The helpline numbers in
    # app/safety/responses/ are the riskiest lines in this repository —
    # a wrong number costs somebody in crisis the one attempt they were
    # willing to make — so a region whose numbers no human has dialled
    # must fail at startup rather than serve Indian numbers to someone
    # who cannot call them.
    crisis_helpline_region: Literal["IN"] = "IN"

    # ─── Embeddings ───────────────────────────────────────────────
    embedding_provider: str = "ollama"
    embedding_model: str = "nomic-embed-text"
    # Never hardcode this anywhere else. nomic-embed-text is 768; most
    # hosted models are 1024. A pgvector column is fixed-dimension, so a
    # literal in a migration means re-embedding the whole corpus later.
    embedding_dim: int = Field(default=768, ge=64, le=4096)

    @property
    def effective_llm_timeout_seconds(self) -> float:
        """The configured budget, floored for a paid provider.

        An operator who deliberately raises LLM_TIMEOUT_SECONDS keeps
        their value; one who leaves the documented default does not
        silently get a shorter budget than the adapter was written for.
        """
        if self.llm_provider_tier == "paid":
            return max(self.llm_timeout_seconds, self.llm_paid_timeout_floor_seconds)
        return self.llm_timeout_seconds

    @property
    def is_production(self) -> bool:
        return self.env == "production"


settings = Settings()
