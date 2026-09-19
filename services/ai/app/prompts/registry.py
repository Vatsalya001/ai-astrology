"""Prompts as versioned, immutable artifacts.

── Why immutable ──

"You cannot debug a bad response from three weeks ago if the prompt that
produced it has been edited since." A response records its
`prompt_version`; that only means anything if `v1` today is byte-for-byte
`v1` in September. So `chat_response.v1` is frozen and improvements
become `v2`, enforced by a committed hash per module rather than by
discipline.

Rollback is then a config change and an A/B test is a percentage split,
both without a deploy.

── Why ordering is a first-class concern ──

Prompt caching matches a byte-identical PREFIX. Stable content first,
volatile last: a single byte changing early invalidates everything
downstream, so putting a timestamp at the top silently turns every
request into a cache miss and multiplies the bill. `PromptBuilder`
enforces the order structurally — it is not possible to add a stable
block after a volatile one.
"""

from __future__ import annotations

import hashlib
from pathlib import Path

from pydantic import BaseModel

from app.providers.base import SystemBlock

MODULES_DIR = Path(__file__).parent / "modules"


class PromptModuleNotFoundError(KeyError):
    """A named module/version that is not on disk.

    Its own type because the recovery differs from any other KeyError: a
    missing prompt module means a deploy is incomplete or a version was
    renamed, and silently substituting a default would produce a model
    call with the safety rules quietly absent.
    """


class PromptModule(BaseModel):
    name: str
    version: str
    content: str

    @property
    def digest(self) -> str:
        """SHA-256 of the content, used by the immutability test."""
        return hashlib.sha256(self.content.encode()).hexdigest()

    @property
    def qualified(self) -> str:
        return f"{self.name}.{self.version}"


def load_module(name: str, version: str) -> PromptModule:
    """Read one module from disk.

    Personas live in a subdirectory, so a name containing `/` is resolved
    relative to `modules/` — `personas/vedic_guide`. Path traversal is
    refused rather than sanitised: a prompt name is never user input, so
    anything containing `..` is a bug, and quietly stripping it would
    hide the bug instead of surfacing it.
    """
    if ".." in name or name.startswith("/"):
        raise PromptModuleNotFoundError(
            f"refusing to resolve prompt module {name!r}: a module name is never "
            f"user input, so a traversal in one is a bug rather than an attack to "
            f"sanitise away"
        )

    path = MODULES_DIR / f"{name}.{version}.md"
    if not path.exists():
        raise PromptModuleNotFoundError(
            f"no prompt module at {path}. A missing module must not fall back to a "
            f"default — that would silently send a request with the safety rules "
            f"absent."
        )

    return PromptModule(name=name, version=version, content=path.read_text().strip())


def all_modules() -> list[PromptModule]:
    """Every module on disk, sorted, for the immutability test."""
    found: list[PromptModule] = []
    for path in sorted(MODULES_DIR.rglob("*.md")):
        relative = path.relative_to(MODULES_DIR)
        # `astrology_rules.v1.md` → name `astrology_rules`, version `v1`.
        stem = relative.as_posix().removesuffix(".md")
        name, _, version = stem.rpartition(".")
        found.append(PromptModule(name=name, version=version, content=path.read_text().strip()))
    return found


class BuilderOrderError(RuntimeError):
    """A stable block was added after the cache breakpoint.

    Structural rather than advisory: the ordering rule is the single
    biggest cost lever in the service, and "remember to add stable
    content first" is exactly the kind of instruction that survives
    review and not the next refactor.
    """


class PromptBuilder:
    """Composes a system prompt with the cacheable prefix first.

    The builder has two phases and cannot go back: stable blocks, then
    `cache_breakpoint()`, then volatile ones. Calling `add()` after the
    breakpoint raises — which converts a silent cost regression into a
    failing test.
    """

    def __init__(self) -> None:
        self._stable: list[SystemBlock] = []
        self._volatile: list[SystemBlock] = []
        self._broken = False
        self._versions: dict[str, str] = {}

    # ─── stable prefix ───────────────────────────────────────────────

    def add(self, name: str, version: str) -> PromptBuilder:
        if self._broken:
            raise BuilderOrderError(
                f"{name}.{version} was added after the cache breakpoint. Everything "
                f"before the breakpoint is cached and everything after is not, so a "
                f"stable module placed here is paid for on every single request."
            )
        module = load_module(name, version)
        self._versions[module.name] = module.version
        self._stable.append(
            SystemBlock(content=module.content, name=module.qualified, cacheable=True)
        )
        return self

    def persona(self, persona: str, version: str) -> PromptBuilder:
        return self.add(f"personas/{persona}", version)

    def cache_breakpoint(self) -> PromptBuilder:
        if self._broken:
            raise BuilderOrderError("cache_breakpoint() called twice")
        if not self._stable:
            raise BuilderOrderError(
                "cache_breakpoint() with nothing before it. A breakpoint at position "
                "zero caches nothing and costs a round trip to discover that."
            )
        self._broken = True
        return self

    # ─── volatile suffix ─────────────────────────────────────────────

    def _volatile_block(self, name: str, content: str) -> PromptBuilder:
        if not self._broken:
            raise BuilderOrderError(
                f"{name} was added before the cache breakpoint. Volatile content in "
                f"the cached prefix changes the prefix on every request, so nothing "
                f"is ever a cache hit."
            )
        if content.strip():
            self._volatile.append(SystemBlock(content=content.strip(), name=name))
        return self

    def chart_context(self, content: str) -> PromptBuilder:
        return self._volatile_block("chart_context", content)

    def user_context(self, content: str) -> PromptBuilder:
        return self._volatile_block("user_context", content)

    def conversation_context(self, summary: str, recent: str = "") -> PromptBuilder:
        joined = "\n\n".join(part for part in (summary, recent) if part.strip())
        return self._volatile_block("conversation_context", joined)

    def knowledge_context(self, content: str) -> PromptBuilder:
        return self._volatile_block("knowledge_context", content)

    # ─── result ──────────────────────────────────────────────────────

    def build(self) -> list[SystemBlock]:
        if not self._broken:
            raise BuilderOrderError(
                "build() without a cache_breakpoint(). Without one, no provider knows "
                "where the stable prefix ends, and the largest cost lever in this "
                "service is silently off."
            )
        return [*self._stable, *self._volatile]

    @property
    def versions(self) -> dict[str, str]:
        """Which version of each module went in.

        Recorded on the response so "why did the AI say that?" has an
        answer. A response that names only the model cannot be explained
        once the prompts have moved on.
        """
        return dict(self._versions)

    @property
    def cacheable_prefix(self) -> str:
        """Exactly the bytes a provider may cache.

        Exposed so the prefix-stability test can compare two independent
        builds without reaching into private state — the property being
        tested is that these bytes do not move, and a test that reads
        `_stable` would keep passing if the join changed.
        """
        return "\n\n".join(block.content for block in self._stable)
