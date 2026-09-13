"""Structural guard: astro-service must never be able to call an LLM.

The determinism principle says a language model must never compute a
planetary position, a house, a nakshatra or a dasha. Prose in a spec
does not enforce that. This test does.

Three independent barriers exist in total:
  1. This test (CI fails if a model SDK appears)
  2. No model API key in the service's environment
  3. Container egress deny-list in deployment

If a future change makes this test fail, the correct response is almost
never to relax the test — it is to move that work into ai-service.
"""

from __future__ import annotations

import ast
import importlib.util
from pathlib import Path

APP_DIR = Path(__file__).resolve().parent.parent / "app"

# Module path prefixes that can reach a language model.
#
# Matched as dotted prefixes, not bare roots: "google.generativeai" is
# banned but "google.protobuf" is not, and a bare-root check would
# conflate the two and make this guard flaky.
BANNED_MODULE_PREFIXES = (
    "anthropic",
    "openai",
    "google.generativeai",
    "google.genai",
    "litellm",
    "langchain",
    "llama_index",
    "transformers",
    "ollama",
    "cohere",
    "mistralai",
    "sentence_transformers",
    "vertexai",
)

# Packages that would let this service make arbitrary outbound calls.
# It is supposed to be offline; allowing an HTTP client back in is how
# the first barrier quietly erodes.
BANNED_NETWORK_PREFIXES = ("httpx", "requests", "aiohttp", "urllib3")


def _imported_modules(path: Path) -> set[str]:
    """Every absolute module path imported by a Python file."""
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    modules: set[str] = set()

    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                modules.add(alias.name)
        # node.level > 0 means a relative import: nothing absolute to check.
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            modules.add(node.module)

    return modules


def _matches(module: str, prefixes: tuple[str, ...]) -> str | None:
    """Return the banned prefix this module falls under, if any."""
    for prefix in prefixes:
        if module == prefix or module.startswith(prefix + "."):
            return prefix
    return None


def _app_files() -> list[Path]:
    return sorted(APP_DIR.rglob("*.py"))


def _scan(prefixes: tuple[str, ...]) -> list[str]:
    offenders: list[str] = []
    for path in _app_files():
        hits = {m for mod in _imported_modules(path) if (m := _matches(mod, prefixes))}
        if hits:
            rel = path.relative_to(APP_DIR.parent)
            offenders.append(f"{rel}: {', '.join(sorted(hits))}")
    return offenders


def test_app_directory_is_not_empty() -> None:
    """Guard against the guard: an empty directory would pass vacuously."""
    assert _app_files(), (
        f"no Python files found under {APP_DIR} — "
        "this guard would pass vacuously, which is worse than failing"
    )


def test_no_llm_sdk_imports() -> None:
    """No module in app/ may import a language-model SDK."""
    offenders = _scan(BANNED_MODULE_PREFIXES)

    assert not offenders, (
        "astro-service imported a language-model SDK:\n  "
        + "\n  ".join(offenders)
        + "\n\nThis service must remain deterministic and AI-free. "
        "Move this work to ai-service."
    )


def test_no_outbound_http_clients() -> None:
    """astro-service is offline. No general-purpose HTTP clients in app/."""
    offenders = _scan(BANNED_NETWORK_PREFIXES)

    assert not offenders, (
        "astro-service imported an HTTP client:\n  "
        + "\n  ".join(offenders)
        + "\n\nThis service is stateless and offline by design. "
        "(httpx is a dev dependency for the test client only.)"
    )


def test_no_llm_sdk_installed() -> None:
    """Belt and braces: the SDKs should not even be installed here.

    Catches a dependency added to pyproject.toml but not yet imported —
    the barrier should fail at the earliest possible point.
    """
    installed = sorted(p for p in BANNED_MODULE_PREFIXES if _is_importable(p))

    assert not installed, (
        f"language-model SDK(s) present in the astro-service environment: {installed}. "
        "Remove them from pyproject.toml."
    )


def _is_importable(dotted: str) -> bool:
    try:
        return importlib.util.find_spec(dotted) is not None
    except (ImportError, ValueError, ModuleNotFoundError):
        return False
