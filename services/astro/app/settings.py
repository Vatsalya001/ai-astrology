"""Configuration for astro-service.

Loaded once at import time. A missing or malformed value raises
immediately, so the container fails to start rather than failing on the
first real request.

`extra="forbid"` means a typo'd environment variable is an error rather
than a silently ignored setting — worth having when a wrong ayanamsa
would quietly produce wrong charts for every user.
"""

from typing import Literal

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="forbid",
        case_sensitive=False,
    )

    env: Literal["development", "staging", "production"] = "development"
    astro_port: int = 8100
    log_level: Literal["debug", "info", "warning", "error"] = "info"

    # Shared secret for service-to-service auth. api-service sends this
    # on every call; anything without it is rejected.
    internal_token: str = "dev-internal-token-change-me"

    # ─── Ephemeris (consumed in Phase 2) ──────────────────────────
    # moseph = Moshier analytical ephemeris, built into Swiss Ephemeris.
    # Needs no data files and is accurate to ~0.1 arcsecond for
    # 1800-2200, which is far beyond what astrology requires.
    ephemeris_flag: Literal["moseph", "swieph"] = "moseph"
    ephemeris_path: str = "./data/ephe"

    default_ayanamsa: Literal["lahiri", "raman", "kp"] = "lahiri"
    default_house_system: Literal["whole_sign", "placidus", "sripati"] = "whole_sign"
    default_node_type: Literal["mean", "true"] = "mean"

    # NOTE: there is deliberately no LLM configuration here, and there
    # never will be. See the dependency policy in pyproject.toml.

    @property
    def is_production(self) -> bool:
        return self.env == "production"


settings = Settings()
