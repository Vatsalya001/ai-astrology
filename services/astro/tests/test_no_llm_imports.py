"""Structural guard: astro-service must never be able to call an LLM.

The determinism principle says a language model must never compute a
planetary position, a house, a nakshatra or a dasha. Prose in a spec
does not enforce that. This test does.

TWO independent barriers exist. This docstring said three until
2026-09-23, and the third did not exist:

  1. This test — CI fails if a model SDK, an HTTP client or a dynamic
     import appears in app/.
  2. No model API key in the service's environment. Verified against the
     running container, not just the compose file: INTERNAL_TOKEN, ENV,
     LOG_LEVEL and PATH, and no LLM/OPENAI/GROQ/GOOGLE/ANTHROPIC key of
     any kind. docker-compose.yml says why, in a comment.
  3. ~~Container egress deny-list in deployment.~~ **There is none.**
     No `networks:` block, no `internal: true`, no network policy, no
     firewall config anywhere in the repo — the container runs on the
     default bridge with unrestricted outbound.

That matters more than a documentation slip, because this file is where
a future maintainer comes to find out how much slack barrier 1 has. The
honest answer is: none. It is the only barrier that stops code, and for
a while it was not stopping much — `urllib.request`, `socket`,
`subprocess` and `importlib.import_module("openai")` all walked past it.

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
    # Sibling of the two paths above, and the one an audit reached for
    # first precisely because it was not listed.
    "google.ai",
    "google.cloud.aiplatform",
    "litellm",
    "langchain",
    "llama_index",
    "transformers",
    "ollama",
    "cohere",
    "mistralai",
    "sentence_transformers",
    "vertexai",
    # Hosted inference the original list did not name. `groq` is the
    # sharpest omission: this project's OWN accuracy measurements run on
    # Groq, so it is the single most likely SDK for someone to reach for.
    # `boto3` is here for Bedrock — an AWS SDK is not obviously a model
    # SDK, which is exactly why it needs naming.
    "groq",
    "boto3",
    "replicate",
    "together",
    "fireworks",
    "huggingface_hub",
    "cerebras",
)

# General-purpose HTTP clients. Allowing one back in is how the first
# barrier quietly erodes: today it fetches a timezone database, tomorrow
# someone points it at a model endpoint.
#
# The stdlib entries are not padding. An audit put each of these into
# `app/` in turn and the suite stayed green for every one:
#
#     import urllib.request        a complete HTTP client, no dependency
#     import http.client           the layer under it
#     import socket                the layer under that
#     import subprocess            subprocess.run(["curl", "https://api..."])
#
# A ban list that stops `httpx` and waves through `urllib.request` is not
# a ban on HTTP clients; it is a ban on the convenient ones. `urllib` is
# banned only at `.request` — `urllib.parse` is string manipulation and
# is used legitimately.
BANNED_NETWORK_PREFIXES = (
    "httpx",
    "httpcore",
    "requests",
    "niquests",
    "aiohttp",
    "urllib3",
    "pycurl",
    "urllib.request",
    "http.client",
    "socket",
    "ftplib",
    "smtplib",
    "subprocess",
)

# Calls that defeat an AST import scan by naming the module at runtime.
#
# `importlib.import_module("openai")` reaches the same SDK while this
# file's walker sees only the string "importlib", and `__import__` does
# the same in one builtin. Neither has any legitimate use in a service
# whose imports are all static.
BANNED_DYNAMIC_IMPORTS = ("importlib.import_module", "__import__")

# The ONE permitted egress path, and the reasoning for it.
#
# The invariant that matters is that this service cannot call a language
# model. "Offline" is the means, not the end. Exporting spans to a
# collector you operate is categorically different from calling a
# third-party model API: the destination is your own infrastructure, it
# carries no request content, and it is disabled unless
# OTEL_EXPORTER_OTLP_ENDPOINT is set.
#
# Excluding this service from tracing would leave a hole in exactly the
# cross-process visibility tracing exists to provide.
#
# This allowance is narrow on purpose. It permits the OTLP exporter and
# nothing else — not the `requests` library the exporter happens to use
# internally, which remains banned from app/ above.
PERMITTED_EGRESS_PREFIXES = (
    "opentelemetry",
    # Error reporting, and it was ALREADY HERE — app/telemetry.py has
    # imported and initialised it since this service was written, while
    # the test below claimed to catch exactly that and could not fail.
    # Listing it is not a widening; it is writing down what shipped.
    #
    # It qualifies under the same reasoning as the OTLP exporter: the
    # destination is infrastructure you operate, and it is inert unless
    # SENTRY_DSN is set. It is a weaker case, because an exception
    # payload CAN carry content an OTLP span does not — which is why
    # app/telemetry.py sets `send_default_pii=False` and passes
    # `before_send=_scrub_event`. If either of those is ever removed,
    # this entry stops being justifiable and should come out.
    "sentry_sdk",
)


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
    """No general-purpose HTTP client may be imported in app/.

    The narrow exception for telemetry export is asserted separately
    below, so that widening it requires editing a test that says out
    loud what is being widened.
    """
    offenders = _scan(BANNED_NETWORK_PREFIXES)

    assert not offenders, (
        "astro-service imported a general-purpose HTTP client:\n  "
        + "\n  ".join(offenders)
        + "\n\nThis service performs no outbound requests except optional "
        "telemetry export. (httpx is a dev dependency for the test client only.)"
    )


def test_telemetry_is_the_only_egress() -> None:
    """Pin the exception so it cannot silently widen.

    If a second egress path is ever added, this test fails and whoever
    added it has to state the justification here rather than quietly
    relying on the HTTP-client ban not covering their choice.

    ── This test used to be incapable of failing ──

    It was written as:

        for module in imports:
            if _matches(module, PERMITTED_EGRESS_PREFIXES):
                egress_modules.add(module.split(".")[0])
        assert egress_modules <= {"opentelemetry"}

    A module entered the set only by matching the prefix "opentelemetry",
    so its `split(".")[0]` was necessarily "opentelemetry" and the set was
    a subset of the permitted one BY CONSTRUCTION. No import could ever
    fail it. Adding `import sentry_sdk` to `app/` left it at 5 passed —
    and `sentry_sdk` was already there, so the one thing this test
    existed to notice had already happened and it was green.

    The filter and the assertion have to use DIFFERENT lists: scan for
    anything that can open a socket, then assert what was found is
    permitted.
    """
    # Everything egress-capable: the banned clients, the model SDKs, and
    # the permitted paths. The union is the point — a filter that only
    # admits permitted modules can only ever find permitted modules.
    known_egress = BANNED_NETWORK_PREFIXES + BANNED_MODULE_PREFIXES + PERMITTED_EGRESS_PREFIXES

    found: dict[str, str] = {}
    for path in _app_files():
        for module in _imported_modules(path):
            if (hit := _matches(module, known_egress)) is not None:
                found[hit] = str(path.relative_to(APP_DIR.parent))

    unexpected = {
        prefix: where
        for prefix, where in found.items()
        if _matches(prefix, PERMITTED_EGRESS_PREFIXES) is None
    }

    assert not unexpected, (
        f"unexpected egress-capable imports: {sorted(unexpected)} "
        f"(first seen in {sorted(unexpected.values())}). astro-service may reach a "
        "telemetry collector and nothing else — see PERMITTED_EGRESS_PREFIXES."
    )


def test_the_egress_test_can_actually_fail() -> None:
    """The guard on the guard, because this one was vacuous for real.

    Asserts the mechanism above is sensitive to an import that is NOT on
    the permitted list. Without this, the same structural mistake — a
    filter and an assertion sharing one list — reads as perfectly
    sensible code and passes review again.
    """
    known_egress = BANNED_NETWORK_PREFIXES + BANNED_MODULE_PREFIXES + PERMITTED_EGRESS_PREFIXES

    # A module that is egress-capable and not permitted must be seen by
    # the scan AND rejected by the permit check.
    assert _matches("openai", known_egress) == "openai"
    assert _matches("openai", PERMITTED_EGRESS_PREFIXES) is None

    assert _matches("urllib.request", known_egress) == "urllib.request"
    assert _matches("urllib.request", PERMITTED_EGRESS_PREFIXES) is None

    # And a permitted one must survive both, or the test above would fail
    # on the telemetry this service is allowed to send.
    assert _matches("opentelemetry.sdk.trace", known_egress) == "opentelemetry"
    assert _matches("opentelemetry.sdk.trace", PERMITTED_EGRESS_PREFIXES) == "opentelemetry"


def test_dynamic_imports_are_not_used() -> None:
    """`importlib.import_module("openai")` defeats every check above.

    `_imported_modules` walks AST import nodes, so a runtime import is
    invisible to it: the walker sees the string "importlib" and nothing
    else. An audit confirmed both forms slip through untouched.

    This service's imports are all static. There is no legitimate reason
    for either call here, so banning them outright costs nothing and
    closes the hole rather than trying to evaluate the argument.
    """
    offenders: list[str] = []
    for path in _app_files():
        source = path.read_text(encoding="utf-8")
        tree = ast.parse(source, filename=str(path))
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            target = node.func
            name = ""
            if isinstance(target, ast.Name):
                name = target.id
            elif isinstance(target, ast.Attribute) and isinstance(target.value, ast.Name):
                name = f"{target.value.id}.{target.attr}"
            if name in BANNED_DYNAMIC_IMPORTS:
                rel = path.relative_to(APP_DIR.parent)
                offenders.append(f"{rel}:{node.lineno}: {name}(...)")

    assert not offenders, (
        "astro-service used a dynamic import:\n  "
        + "\n  ".join(offenders)
        + "\n\nRuntime imports are invisible to the static scans in this file, "
        "which is the entire reason they are banned."
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
