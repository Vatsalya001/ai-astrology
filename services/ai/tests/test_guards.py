"""Tests for the startup guards.

A guard that has never been observed to fire is a guard you should not
trust. Every one of these asserts the *negative* case — that the unsafe
configuration is actually refused.
"""

from __future__ import annotations

import pytest

from app.guards import (
    DEV_INTERNAL_TOKEN,
    UnsafeConfigurationError,
    assert_database_is_read_only,
    assert_internal_token_changed,
    assert_provider_allowed,
)


class TestProviderPIIGuard:
    """Production must use a paid provider. No exceptions."""

    @pytest.mark.parametrize("tier", ["local", "free-hosted"])
    def test_production_rejects_non_paid_provider(self, tier: str) -> None:
        with pytest.raises(UnsafeConfigurationError, match="Refusing to start"):
            assert_provider_allowed("ollama", tier, "production")  # type: ignore[arg-type]

    def test_production_accepts_paid_provider(self) -> None:
        assert_provider_allowed("anthropic", "paid", "production")

    @pytest.mark.parametrize("env", ["development", "staging"])
    @pytest.mark.parametrize("tier", ["local", "free-hosted", "paid"])
    def test_non_production_accepts_any_tier(self, env: str, tier: str) -> None:
        """Free models are the whole point of local development."""
        assert_provider_allowed("ollama", tier, env)  # type: ignore[arg-type]

    def test_error_names_the_offending_provider_and_tier(self) -> None:
        """A startup failure must say what is wrong, not just that it is."""
        with pytest.raises(UnsafeConfigurationError) as exc:
            assert_provider_allowed("groq", "free-hosted", "production")

        message = str(exc.value)
        assert "groq" in message
        assert "free-hosted" in message
        assert "LLM_PROVIDER" in message  # tells you which var to change


class TestInternalTokenGuard:
    def test_production_rejects_dev_token(self) -> None:
        with pytest.raises(UnsafeConfigurationError, match="INTERNAL_TOKEN"):
            assert_internal_token_changed(DEV_INTERNAL_TOKEN, "production")

    def test_production_accepts_real_token(self) -> None:
        assert_internal_token_changed("f3a9c1e0b7d2", "production")

    def test_development_allows_dev_token(self) -> None:
        assert_internal_token_changed(DEV_INTERNAL_TOKEN, "development")


class TestReadOnlyDatabaseGuard:
    def test_production_rejects_writable_role(self) -> None:
        with pytest.raises(UnsafeConfigurationError, match="read-only"):
            assert_database_is_read_only("postgresql://astro:astro@db:5432/astro", "production")

    def test_production_accepts_read_only_role(self) -> None:
        assert_database_is_read_only("postgresql://astro_ro:secret@db:5432/astro", "production")

    def test_development_is_not_checked(self) -> None:
        """Locally, pointing at the writable role is inconvenient, not dangerous."""
        assert_database_is_read_only("postgresql://astro:astro@localhost/astro", "development")
