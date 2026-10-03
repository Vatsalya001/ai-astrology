"""Configuration for ai-service."""

from typing import Literal

from pydantic import Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# A provider's tier determines whether it may be used in production.
#   local       — runs on this machine, nothing leaves it (Ollama)
#   free-hosted — a free API tier; may train on inputs
#   paid        — commercial tier with a no-training commitment
ProviderTier = Literal["local", "free-hosted", "paid"]

# Per-provider tier→model defaults, as (fast, chat, deep).
#
# WHY THIS EXISTS. The three model names used to be plain field defaults
# naming Ollama tags, which made `LLM_PROVIDER=google` plus a key — the
# entire documented setup for a free Google run — send the model name
# `llama3.2:3b` to the Gemini API. That is a 404 `model not found`, and
# the obvious reading of it is "my key is bad" or "the adapter is
# broken". Neither is true, and both cost an hour.
#
# A model name is not really provider-independent configuration: it is
# part of naming the provider. So selecting a provider selects its
# models, and anything explicitly configured still wins (see
# `_default_models_to_the_provider`).
DEFAULT_MODELS: dict[str, tuple[str, str, str]] = {
    # Ollama. Small enough to run on a laptop; §11's documented set.
    "openai-compatible": ("llama3.2:3b", "qwen2.5:7b", "qwen2.5:7b"),
    # §3 routes by job: classification and extraction go to the cheapest
    # model that can do them, conversation to the middle one, and paid
    # interpretation to the largest.
    #
    # THESE NAMES WERE 2.5-flash/2.5-pro AND A NEW KEY COULD NOT CALL
    # THEM. Google's model LIST still returns `gemini-2.5-flash` — for
    # existing users — while `generateContent` answers:
    #
    #   404 NOT_FOUND. This model models/gemini-2.5-flash is no longer
    #   available to new users. Please update your code to use
    #   models/gemini-3.6-flash
    #
    # So the model appeared available, was not, and the failure arrived
    # as a 404 that reads like a typo in a config file. Nothing offline
    # can catch that: it is a fact about the vendor's account policy, not
    # about our code. `scripts/verify_provider.py google` caught it on
    # its first real run, which is the entire argument for that script
    # existing.
    #
    # `deep` is Pro, which returns 429 "exceeded your current quota" on a
    # free key — expected, and not a bug: `deep` IS the paid
    # interpretation tier. A free-hosted run exercises fast and chat.
    # Verified by probing every candidate against a real key rather than
    # by reading the model list, because the list is what lied.
    "google": ("gemini-3.5-flash-lite", "gemini-3.6-flash", "gemini-3.1-pro-preview"),
    # MockProvider ignores this map and reports `mock-{tier}` from the
    # fixture. These are those names, so `describe()` prints what the
    # responses will actually say — and so they are priced, since an
    # unpriced model raises.
    "mock": ("mock-fast", "mock-chat", "mock-deep"),
}


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
    llm_provider: Literal["openai-compatible", "google", "mock"] = "openai-compatible"
    llm_base_url: str = "http://localhost:11434/v1"
    llm_api_key: str = ""
    llm_provider_tier: ProviderTier = "local"

    # These defaults are the `openai-compatible` row of DEFAULT_MODELS,
    # repeated here only because a field needs a default. The provider's
    # row replaces any of the three that configuration did not set —
    # `_default_models_to_the_provider` below.
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
    llm_fallback_provider: Literal["", "google", "mock"] = ""
    llm_fallback_api_key: str = ""

    # ─── Resilience (PHASE-04 §2, task 4.9) ───────────────────────
    # One operator-visible knob per behaviour. The adapters carry their
    # own defaults (60s, 120s) and `ResilientProvider` carries its own
    # budgets; a default that configuration cannot reach is a knob that
    # lies, so app/api/complete.py passes every one of these in.
    intent_min_confidence: float = Field(default=0.4, ge=0.0, le=1.0)
    """Below this, a classification is discarded for broad context.

    §6 specifies 0.6. This ships 0.4, deliberately, and the deviation is
    recorded here rather than buried in a constant.

    WHY IT MOVED. The threshold is applied to a SELF-REPORTED number,
    and small local models do not calibrate one. Measured on 30 deferred
    messages, llama3.2:1b answered correctly 12 times and the product
    delivered 2 — ten right answers thrown away by 0.6.

    WHY IT IS A SETTING. Because 0.4 is tuned to a weak local model and
    a hosted model calibrates differently — measured: `qwen3.8-27b`
    reports an honest 0.3 on genuinely vague messages where
    `llama3.2:3b` reported 0.0 on answers it got right.
    A constant would make re-tuning a deploy; this makes it
    `INTENT_MIN_CONFIDENCE=0.6` in an env file. Phase 6's eval harness is
    where the production value gets chosen on evidence, and this is the
    knob it will turn.

    The safety property §6 actually asks for is BROAD CONTEXT when
    unsure, not a discarded label — see app/orchestrator/context.py.
    Lowering this does not weaken that; it only changes how often the
    label is kept.
    """

    llm_timeout_seconds: float = Field(default=60.0, gt=0)
    """Per-provider HTTP budget. §11's documented default is 60.

    It is a FLOOR for the paid provider rather than an absolute: see
    `effective_llm_timeout_seconds`. The deep tier — a paid
    interpretation on the largest model — is exactly the call that needs
    the longer budget, and a regression that only shows up on the most
    expensive request in the product is the worst kind to ship silently.
    """

    llm_paid_timeout_floor_seconds: float = Field(default=120.0, gt=0)
    """The floor for a paid provider.

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
    prompt_version_intent: str = Field(default="v4", pattern=r"^v[0-9]+$")
    # v2, not the hardcoded v1 the screener used to carry. v1 said only
    # "a single JSON object" and never named `category` or `confidence`,
    # so llama3.2:3b emitted `{}` and once echoed the prompt's own rules
    # back as the answer. Both parsed into a CONFIDENT all-clear.
    #
    # It is a setting at all because the other two are: the safety
    # screener was the one prompt whose version could not be changed
    # without a deploy, which is backwards.
    prompt_version_safety: str = Field(default="v2", pattern=r"^v[0-9]+$")

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

    # CRISIS_HELPLINE_REGION was here, `Literal["IN"]`, and it is gone.
    #
    # It guarded a real risk: app/safety/responses/ held Indian helpline
    # NUMBERS, and serving those to someone who cannot dial them is worse
    # than serving nothing — so a region nobody had verified refused to
    # boot.
    #
    # The numbers were then removed (no human had dialled them, and a
    # test proving a number is PRESENT proves nothing about whether it
    # ANSWERS). The response now points at findahelpline.com, which
    # resolves by country itself and says "your country, in your
    # language" and "your local emergency number".
    #
    # So the setting stopped selecting anything while still refusing to
    # boot outside India — a knob that lies, and one that blocked a
    # deployment it would have served correctly. Its own rejection test
    # said the quiet part: "accepting the others would be a setting that
    # does nothing."
    #
    # Phase 5 may add region-specific responses. It can add the setting
    # back, at the point where it would mean something.

    # ─── Embeddings ───────────────────────────────────────────────
    # A Literal, not a str, for the reason factory.py explains about
    # `llm_provider`: adding a third embedding backend without adding it
    # to `embedding_from_settings` should be a mypy --strict failure at
    # that match statement, not an `else` branch that silently serves the
    # wrong vectors. Wrong vectors do not error — they rank as noise.
    embedding_provider: Literal["ollama", "google"] = "ollama"

    embedding_base_url: str = "http://localhost:11434"
    """Where the Ollama embedding server is. Independent of `llm_base_url`.

    It was derived from `llm_base_url` for about five minutes, on the
    reasoning that both point at the same Ollama. They do not have to:
    hosted chat with local embeddings is a sensible combination and so is
    the reverse, and this project is currently running exactly the first
    — `LLM_PROVIDER=google` with `LLM_BASE_URL=` empty, because the
    Google adapter uses its SDK and needs no URL.

    Deriving it produced `base_url=""`, which does not fail at
    construction. It fails on the first embed call, as a connection error
    naming an empty host, during ingestion of a corpus — the point at
    which the least context is available to interpret it.

    Ignored when `EMBEDDING_PROVIDER=google`.
    """
    embedding_model: str = "nomic-embed-text"
    # Never hardcode this anywhere else. nomic-embed-text is 768; most
    # hosted models are 1024. A pgvector column is fixed-dimension, so a
    # literal in a migration means re-embedding the whole corpus later.
    embedding_dim: int = Field(default=768, ge=64, le=4096)

    # ─── Retrieval (PHASE-05 §9) ──────────────────────────────────
    rag_top_k: int = Field(default=8, ge=1, le=50)
    """How many chunks reach the model.

    §4: "Cap retrieval at 6 to 8 chunks. More context is not better context;
    it dilutes attention and costs tokens." The upper bound here is 50
    rather than unbounded so a mistyped env var cannot quietly send
    thousands of tokens of corpus into every request.
    """

    rag_vector_weight: float = Field(default=0.6, ge=0.0, le=1.0)
    rag_keyword_weight: float = Field(default=0.4, ge=0.0, le=1.0)
    """The hybrid split. They are not required to sum to 1.

    Not normalised, because the scores they weight are not on the same
    scale: a cosine similarity is in [0, 1] and `ts_rank` is typically
    well under 0.1. The weights are therefore a tuning surface rather
    than a probability split, and the retrieval quality set is what says
    whether a pair is any good.
    """

    rag_min_score: float = Field(default=0.10, ge=0.0, le=1.0)
    """Below this combined score a chunk is dropped rather than ranked.

    A floor matters more than a ceiling here. A nearest-neighbour query
    has no concept of "no match" — it always returns its `LIMIT` worth of
    rows — so without a floor, a question the corpus cannot answer still
    retrieves eight chunks, which the model then paraphrases confidently.

    ── Why 0.10, and why it is not comparable to §9's 0.25 ──

    Because the scores it is applied to are normalised, and §9's number
    was written for raw ones. See `VECTOR_BASELINE` and
    `TSRANK_CEILING` in app/retrieval/query.py for why the raw scores
    had to be rescaled at all.

    Swept against the 48-question quality set and the real 593-chunk
    corpus:

        floor   recall@8   off-domain questions still returning chunks
        0.01    1.000      1
        0.03    1.000      1
        0.05    1.000      0
        0.10    1.000      0
        0.15    1.000      0
        0.20    0.958      0
        0.25    0.938      0

    So the usable window is **0.05 to 0.15** — perfect recall with
    nothing off-domain getting through. 0.10 sits inside it with roughly
    2x headroom below and 1.5x above.

    This is worth contrasting with the first attempt, which applied a
    floor to raw cosine scores. There the window was 0.297 to 0.332 —
    about 10% — and a real question ("I keep losing money as fast as I
    make it") fell below a floor set inside it and retrieved nothing at
    all. A threshold with a 3x window is a setting; one with a 10%
    window is a latent bug.
    """

    rag_metadata_boost: float = Field(default=1.35, ge=1.0, le=5.0)
    """Multiplier for chunks matching the user's actual placements.

    §4: "If the chart has Saturn in the 10th, the 'Saturn in 10th house'
    chunk should win regardless of embedding similarity. This small rule
    does more for answer quality than any amount of embedding-model
    tuning."

    Lower bound 1.0, so the setting can disable the boost but cannot
    invert it into a penalty.
    """

    chat_recent_message_window: int = Field(default=6, ge=0, le=50)
    """How many prior messages go into the prompt. §9."""

    @model_validator(mode="after")
    def _default_models_to_the_provider(self) -> "Settings":
        """Fill the tier models nobody configured from the provider's row.

        `model_fields_set` holds only what a source actually supplied —
        an env var, a `.env` line, a keyword — so an operator who names a
        model keeps it, including one that happens to equal the field
        default. Silently overriding an explicit choice would be a worse
        bug than the one this fixes.
        """
        for field, name in zip(
            ("llm_model_fast", "llm_model_chat", "llm_model_deep"),
            DEFAULT_MODELS[self.llm_provider],
            strict=True,
        ):
            if field not in self.model_fields_set:
                # `object.__setattr__`-free: assignment here would
                # re-enter validation, and Settings is not frozen.
                self.__dict__[field] = name
        return self

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
