"""Tests for the startup guards.

A guard that has never been observed to fire is a guard you should not
trust. Every one of these asserts the *negative* case — that the unsafe
configuration is actually refused.
"""

from __future__ import annotations

import re
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pytest
from pydantic import ValidationError

from app.api.complete import get_orchestrator, get_provider_chain
from app.guards import (
    DEV_INTERNAL_TOKEN,
    UnsafeConfigurationError,
    assert_chain_providers_allowed,
    assert_database_is_read_only,
    assert_internal_token_changed,
    assert_provider_allowed,
    assert_safety_layers_enabled,
    run_all_startup_guards,
)
from app.providers import Capabilities, ProviderTier
from app.settings import Settings, settings


class TestProviderPIIGuard:
    """Production must use a paid provider. No exceptions."""

    @pytest.mark.parametrize("tier", ["local", "free-hosted"])
    def test_production_rejects_non_paid_provider(self, tier: str) -> None:
        with pytest.raises(UnsafeConfigurationError, match="Refusing to start"):
            assert_provider_allowed("ollama", tier, "production")  # type: ignore[arg-type]

    def test_production_accepts_paid_provider(self) -> None:
        assert_provider_allowed("some-paid-vendor", "paid", "production")

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


class StubProvider:
    """A provider with only the two fields the guard reads.

    Structural typing, so no base class — and deliberately minimal: if
    the guard ever needs more than an id and a tier to make this
    decision, that is a change worth noticing here.
    """

    def __init__(self, provider_id: str, tier: ProviderTier) -> None:
        self._id = provider_id
        self._tier: ProviderTier = tier

    @property
    def id(self) -> str:
        return self._id

    @property
    def tier(self) -> ProviderTier:
        return self._tier

    @property
    def capabilities(self) -> Capabilities:
        return Capabilities()


class TestChainProviderTierGuard:
    """The tier of the object that serves, not of the setting beside it."""

    def test_production_rejects_a_provider_whose_own_tier_is_not_paid(self) -> None:
        with pytest.raises(UnsafeConfigurationError, match="mock"):
            assert_chain_providers_allowed(
                [StubProvider("mock", "local")],  # type: ignore[list-item]
                "production",
            )

    def test_a_free_fallback_behind_a_paid_primary_is_refused(self) -> None:
        """Every link, not just the first.

        A chain that is paid at the front and free-hosted behind it is a
        free-hosted service the moment the primary has a bad minute.
        """
        with pytest.raises(UnsafeConfigurationError, match="google"):
            assert_chain_providers_allowed(
                [StubProvider("paid-primary", "paid"), StubProvider("google", "free-hosted")],  # type: ignore[list-item]
                "production",
            )

    def test_production_accepts_a_fully_paid_chain(self) -> None:
        assert_chain_providers_allowed(
            [StubProvider("paid-primary", "paid"), StubProvider("paid-eu", "paid")],  # type: ignore[list-item]
            "production",
        )

    def test_development_accepts_a_local_chain(self) -> None:
        assert_chain_providers_allowed(
            [StubProvider("ollama", "local"), StubProvider("mock", "local")],  # type: ignore[list-item]
            "development",
        )


class TestSafetyLayerGuard:
    """§11 documents two switches this phase does not implement as switches."""

    @pytest.mark.parametrize(
        ("validation", "fabricated"),
        [(False, True), (True, False), (False, False)],
    )
    def test_disabling_either_layer_refuses_to_start(
        self, validation: bool, fabricated: bool
    ) -> None:
        with pytest.raises(UnsafeConfigurationError, match="SAFETY_VALIDATION_ENABLED"):
            assert_safety_layers_enabled(validation, fabricated)

    def test_the_documented_defaults_start(self) -> None:
        assert_safety_layers_enabled(True, True)

    def test_the_error_names_the_file_that_would_have_to_change(self) -> None:
        """A refusal that does not say what to do next is just an outage."""
        with pytest.raises(UnsafeConfigurationError) as exc:
            assert_safety_layers_enabled(False, True)

        assert "pipeline.py" in str(exc.value)


@pytest.fixture
def _production_config(monkeypatch: pytest.MonkeyPatch) -> Iterator[None]:
    """A production configuration that passes every guard but the one under test."""
    get_provider_chain.cache_clear()
    get_orchestrator.cache_clear()

    monkeypatch.setattr(settings, "env", "production")
    # Obviously fake rather than hex-shaped. The guard only checks that
    # it DIFFERS from DEV_INTERNAL_TOKEN, so a realistic-looking value
    # bought nothing and tripped the repo's secret scanner — which is
    # the scanner working, and the right fix is the fixture, not an
    # allowlist entry that would blunt it for real credentials too.
    monkeypatch.setattr(settings, "internal_token", "not-the-dev-default-token")
    monkeypatch.setattr(settings, "ai_database_url", "postgresql://astro_ro:s@db:5432/astro")
    monkeypatch.setattr(settings, "safety_validation_enabled", True)
    monkeypatch.setattr(settings, "safety_block_on_fabricated_fact", True)
    monkeypatch.setattr(settings, "llm_fallback_provider", "")

    yield

    get_provider_chain.cache_clear()
    get_orchestrator.cache_clear()


class TestStartupGuardsCheckTheConstructedProvider:
    """PHASE-04 §3 says the guard reads `provider.tier`. Now it does.

    Reading `settings.llm_provider_tier` instead made the guard trust a
    declaration. `MockProvider.tier` is hardcoded `local` precisely so
    the guard refuses it in production — and it booted anyway, because
    nothing ever asked the provider.
    """

    def test_a_mock_provider_declared_paid_is_refused_in_production(
        self, _production_config: None, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        monkeypatch.setattr(settings, "llm_provider", "mock")
        monkeypatch.setattr(settings, "llm_provider_tier", "paid")  # the lie

        with pytest.raises(UnsafeConfigurationError, match='"mock" is tier "local"'):
            run_all_startup_guards()

    def test_the_same_configuration_starts_in_development(
        self, _production_config: None, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Free and mock providers are what development runs on."""
        monkeypatch.setattr(settings, "env", "development")
        monkeypatch.setattr(settings, "llm_provider", "mock")
        monkeypatch.setattr(settings, "llm_provider_tier", "local")

        run_all_startup_guards()

    def test_a_free_tier_fallback_is_refused_in_production(
        self, _production_config: None, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The whole chain is built and checked, not only the primary."""
        # `openai-compatible` + a declared paid tier is the only
        # production-legal primary the settings can express: it is the
        # one adapter with no vendor identity to infer a tier from, so
        # the declaration is the only signal there is.
        monkeypatch.setattr(settings, "llm_provider", "openai-compatible")
        monkeypatch.setattr(settings, "llm_provider_tier", "paid")
        monkeypatch.setattr(settings, "llm_api_key", "")
        monkeypatch.setattr(settings, "llm_fallback_provider", "google")
        monkeypatch.setattr(settings, "llm_fallback_api_key", "not-a-real-key")

        with pytest.raises(UnsafeConfigurationError, match="google"):
            run_all_startup_guards()


# ─── PHASE-04 §11 ────────────────────────────────────────────────────

SPEC = Path(__file__).resolve().parents[3] / "docs" / "specs" / "PHASE-04-AI-INFRASTRUCTURE.md"

DEFERRED_TO_A_LATER_PHASE = {
    # Memory extraction is Phase 6 — PHASE-04 §1 lists "Memory (Phase 6)"
    # as out of scope, and there is no memory prompt module to version.
    # Declaring it would be a setting that promises to select something
    # that does not exist. Until then, a .env copied verbatim from §11
    # must delete this line.
    "PROMPT_VERSION_MEMORY",
}


def _documented_env(section: str = "ai-service") -> dict[str, str]:
    """The §11 block, read from the spec rather than copied out of it.

    Copied lists go stale silently: the spec gains a variable, nobody
    re-reads the test, and `extra="forbid"` turns the new line into a
    boot refusal for whoever deploys next.
    """
    text = SPEC.read_text()
    body = text.split("## 11. Environment variables", 1)[1].split(f"### `{section}`", 1)[1]
    block = body.split("```bash", 1)[1].split("```", 1)[0]

    documented: dict[str, str] = {}
    for line in block.splitlines():
        match = re.match(r"^([A-Z][A-Z0-9_]*)=(.*)$", line.strip())
        if match:
            documented[match.group(1)] = match.group(2).split("#")[0].strip()
    return documented


class TestDocumentedEnvironment:
    """`extra="forbid"` makes an undeclared variable a boot refusal.

    Which means §11 is not documentation, it is a contract: every name
    in it either exists as a field or stops the process. This class is
    that contract, read from the spec at test time.
    """

    def test_the_spec_block_is_actually_found(self) -> None:
        """Or every test below passes while asserting nothing.

        The parse above walks headings and fences that a docs edit could
        move. A silently empty result is exactly the failure this file's
        opening paragraph is about.
        """
        documented = _documented_env()

        assert len(documented) >= 20
        assert "LLM_FALLBACK_PROVIDER" in documented

    def test_every_documented_variable_is_accepted(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """The spec's own `.env`, written to disk and loaded.

        Not a field-name comparison: `extra="forbid"` only fires for
        entries in a `.env` FILE (see app/env_check.py), so the only
        honest reproduction is a file.
        """
        documented = _documented_env()
        usable = {k: v for k, v in documented.items() if k not in DEFERRED_TO_A_LATER_PHASE}

        # The OS environment outranks a dotenv file in pydantic-settings,
        # so a developer with LLM_PROVIDER exported would otherwise be
        # running a different test than CI.
        for name in documented:
            monkeypatch.delenv(name, raising=False)

        env_file = tmp_path / ".env"
        env_file.write_text("".join(f"{name}={value}\n" for name, value in usable.items()))

        loaded = Settings(_env_file=str(env_file))  # type: ignore[call-arg]

        assert loaded.llm_provider == "openai-compatible"
        assert loaded.llm_fallback_provider == ""
        assert loaded.llm_circuit_breaker_threshold == 5

    def test_the_deferred_variables_are_still_undeclared(self) -> None:
        """Keeps the exemption list honest in both directions.

        The day Phase 6 declares `prompt_version_memory`, this fails and
        whoever did it deletes the exemption instead of leaving a stale
        comment claiming the variable is unsupported.
        """
        declared = {name.upper() for name in Settings.model_fields}

        assert DEFERRED_TO_A_LATER_PHASE.isdisjoint(declared)

    def test_an_undeclared_variable_still_refuses_to_start(self, tmp_path: Path) -> None:
        """The negative case for the whole class.

        If `extra="forbid"` were relaxed to make the test above pass,
        every one of these assertions would be vacuous — and a typo'd
        setting would silently run on its default.
        """
        env_file = tmp_path / ".env"
        env_file.write_text("LLM_FALLBCK_PROVIDER=google\n")

        with pytest.raises(ValidationError, match="Extra inputs are not permitted"):
            Settings(_env_file=str(env_file))  # type: ignore[call-arg]

    @pytest.mark.parametrize(
        ("field", "value"),
        [
            # Narrowed on purpose: this phase honours exactly one value,
            # and accepting the others would be a setting that does
            # nothing. See the comments in app/settings.py.
            ("default_persona", "career_guide"),
            # Malformed prompt versions reach the registry as filenames.
            ("prompt_version_chat", "latest"),
            # A negative retry budget is not a smaller one.
            ("llm_max_retries", -1),
            ("llm_timeout_seconds", 0),
            # `openai-compatible` has no second base URL to point at, so
            # it would fall back to the endpoint that just failed.
            ("llm_fallback_provider", "openai-compatible"),
        ],
    )
    def test_a_value_this_phase_cannot_honour_is_refused(self, field: str, value: Any) -> None:
        with pytest.raises(ValidationError):
            Settings(**{field: value})


# ─── the paid-tier timeout floor ─────────────────────────────────────


class TestTheTimeoutFloor:
    """Wiring §11's documented 60s straight through was a regression.

    `AnthropicProvider` carried its own 120s default because a paid
    deep-tier interpretation on the largest model is the longest call
    this service makes. Passing `LLM_TIMEOUT_SECONDS` to every adapter
    halved it — and a regression that only appears on the most expensive
    request in the product is the worst kind to ship silently, because
    the cheap requests that everyone tests with never see it.
    """

    def test_a_paid_provider_gets_the_floor(self) -> None:
        settings = Settings(_env_file=None, llm_provider_tier="paid")  # type: ignore[call-arg]

        assert settings.llm_timeout_seconds == 60.0, "the documented default moved"
        assert settings.effective_llm_timeout_seconds == 120.0

    def test_a_local_provider_does_not(self) -> None:
        """A local model that has not answered in 60s is not going to.

        Waiting two minutes for it would make a dev machine feel broken,
        which is why the floor is tier-conditional rather than a higher
        default for everyone.
        """
        settings = Settings(_env_file=None, llm_provider_tier="local")  # type: ignore[call-arg]

        assert settings.effective_llm_timeout_seconds == 60.0

    def test_an_operator_who_raises_it_keeps_their_value(self) -> None:
        # A floor, not an override. Someone who deliberately set 300
        # should get 300.
        settings = Settings(  # type: ignore[call-arg]
            _env_file=None, llm_provider_tier="paid", llm_timeout_seconds=300.0
        )

        assert settings.effective_llm_timeout_seconds == 300.0

    def test_the_route_uses_the_effective_value(self) -> None:
        """The wiring, not just the property.

        Both tests above would pass with `app/api/complete.py` still
        passing the raw setting — which is exactly the bug.
        """
        # BOTH files. The PRIMARY provider's construction moved to
        # app/providers/factory.py, leaving only the google FALLBACK's
        # timeout in complete.py — so this assertion went on passing on
        # the strength of a path that only takes traffic during an
        # outage, while the primary path went unchecked. A one-line
        # change in factory.py would have put every paid deep-tier
        # interpretation back on a 60s budget instead of the 120s floor,
        # and this test would not have noticed.
        root = Path(__file__).parent.parent / "app"
        sources = {
            "api/complete.py": (root / "api" / "complete.py").read_text(),
            "providers/factory.py": (root / "providers" / "factory.py").read_text(),
        }

        for name, source in sources.items():
            assert "settings.llm_timeout_seconds" not in source, (
                f"{name} passes the raw setting, so a paid provider still gets 60s"
            )
        assert sum(s.count("effective_llm_timeout_seconds") for s in sources.values()) >= len(
            sources
        ), "every provider construction site must use the floored value"


class TestTheExampleFileIsComplete:
    """`.env.example` is what an operator copies. It must be current.

    The existing contract runs ONE direction — every documented variable
    is accepted. Nothing asserted the reverse, so a setting could ship
    invisible, and two did:

    - `PROMPT_VERSION_SAFETY` appeared in no example file at all, so the
      only way to discover it was to read `app/settings.py`.
    - `PROMPT_VERSION_INTENT` sat at `v2` long after `v4` shipped.
      Copying the documented example downgraded the classifier from
      **90.0% to 67.0%** on the labelled set, because v2 hands the model
      a literal `"confidence": 0.0` to copy — and it copies it.

    The second is why this file matters more than the spec: the spec is
    a historical planning document and is allowed to describe the past.
    `.env.example` is an instruction, and a stale instruction is a wrong
    one.
    """

    EXAMPLE = Path(__file__).resolve().parent.parent / ".env.example"

    def _example_values(self) -> dict[str, str]:
        """Only ACTIVE assignments — what you get if you copy the file."""
        found: dict[str, str] = {}
        for line in self.EXAMPLE.read_text().splitlines():
            match = re.match(r"^([A-Z][A-Z0-9_]*)=(.*)$", line.strip())
            if match:
                found[match.group(1)] = match.group(2).split("#")[0].strip()
        return found

    def _example_names(self) -> set[str]:
        """Every name an operator can SEE, commented ones included.

        `LLM_MODEL_FAST` and its siblings ship commented out on purpose —
        pinning them is a trap that sends an Ollama tag to a hosted
        vendor. They are still visible, still documented, and still
        configurable, so a completeness check must count them.
        """
        names = set(self._example_values())
        for line in self.EXAMPLE.read_text().splitlines():
            match = re.match(r"^#\s*([A-Z][A-Z0-9_]*[A-Z0-9])=", line.strip())
            # An env var here always has an underscore; requiring one
            # keeps ordinary prose that happens to contain "WORD=" out.
            if match and "_" in match.group(1):
                names.add(match.group(1))
        return names

    def test_the_parse_finds_something(self) -> None:
        # Or every assertion below is vacuous — the same trap the §11
        # block parser guards against.
        assert len(self._example_values()) >= 15

    def test_every_setting_appears_in_the_example(self) -> None:
        documented = self._example_names()
        declared = {name.upper() for name in Settings.model_fields}

        missing = sorted(declared - documented)

        assert missing == [], (
            f"these settings exist but appear in no example: {missing}. An operator "
            f"cannot configure what they cannot see, and the only way to find them is "
            f"to read app/settings.py."
        )

    def test_the_example_pins_the_versions_that_ship(self) -> None:
        """The specific failure that motivated this class.

        A version drifting behind is not cosmetic: v2 scores 67.0% where
        v4 scores 90.0%, so copying a stale example silently costs 23
        points of accuracy on the gate's own metric.
        """
        values = self._example_values()
        defaults = Settings(_env_file=None)  # type: ignore[call-arg]

        for var, field in (
            ("PROMPT_VERSION_CHAT", "prompt_version_chat"),
            ("PROMPT_VERSION_INTENT", "prompt_version_intent"),
            ("PROMPT_VERSION_SAFETY", "prompt_version_safety"),
        ):
            assert values[var] == getattr(defaults, field), (
                f"{var} is {values[var]} in .env.example but the service ships "
                f"{getattr(defaults, field)}. Copying the example changes behaviour."
            )

    def test_no_example_line_names_a_removed_setting(self) -> None:
        """A commented-out variable is still an instruction.

        `CRISIS_HELPLINE_REGION` was removed and its explanatory comment
        stayed, describing a knob that no longer exists. Prose about a
        dead setting is fine — an assignable line for one is not.
        """
        declared = {name.upper() for name in Settings.model_fields}

        for line in self.EXAMPLE.read_text().splitlines():
            match = re.match(r"^#\s*([A-Z][A-Z0-9_]*[A-Z0-9])=", line.strip())
            # An env var always has an underscore. Without this, ordinary
            # prose trips it — "# INTENT=v2 long after v4 shipped" in the
            # comment above PROMPT_VERSION_INTENT did exactly that.
            if not match or "_" not in match.group(1):
                continue
            assert match.group(1) in declared, (
                f"{line.strip()!r} offers a setting that does not exist"
            )
