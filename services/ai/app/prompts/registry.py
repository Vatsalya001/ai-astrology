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


# Anthropic ignores `cache_control` on a prefix below roughly this many
# tokens — 2048 on Haiku. Below it the breakpoint is a no-op: no write,
# no read, and no error saying so.
#
# TODAY'S PREFIX IS ~770 TOKENS, so the breakpoint this class is built
# around does not engage on any tier. That is not a bug in the builder;
# it is the honest state of Phase 4, where the stable prefix is five
# short modules. PHASE-04 §2 expects "the astrology rule corpus" to be
# large, and Phase 5's RAG corpus is what takes it past the threshold.
#
# Recorded here, and asserted by `test_prompt_registry.py`, because
# §15 names "prompt caching silently stops working in prod" as a risk —
# and a cache that never STARTED working looks identical from the
# outside. When Phase 5 pushes the prefix over the line, that test
# fails and somebody notices it began working.
ANTHROPIC_MIN_CACHEABLE_TOKENS = 1024


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

    def instruction(self, content: str) -> PromptBuilder:
        """A post-breakpoint block that is an INSTRUCTION, not data.

        The safety posture and the corrective retry both live here. They
        vary per request, so they cannot sit in the cached prefix — but
        they are still things the model is told to do, and a response
        that quotes one back is a prompt leak.

        Separated from the data blocks below because the leak check
        needs exactly this distinction. Instructions must never be
        echoed; the user's own chart is MEANT to be reflected back, and
        a leak check that covered it would block the product's main job.
        """
        return self._volatile_block("instruction", content)

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
    def leakable(self) -> str:
        """Everything the model must never quote back.

        The stable prefix PLUS the post-breakpoint instructions — not
        the chart, conversation or knowledge blocks.

        The validator was built from `cacheable_prefix` alone, so
        anything after the breakpoint went unchecked. That covered the
        safety posture ("Do not name a condition") and the corrective
        retry instruction, both of which are instructions a response
        must not repeat. §14 asks for "prompt_leak validation active on
        ALL output"; it was active on part of the prompt.

        The data blocks are deliberately excluded. A user's chart is
        theirs and discussing it back to them is the product's entire
        job — a leak check covering it would block every correct
        reading.
        """
        instructions = [
            block.content
            for block in (*self._stable, *self._volatile)
            if block.cacheable or block.name == "instruction"
        ]
        return "\n\n".join(instructions)

    @property
    def cacheable_prefix_tokens(self) -> int:
        """A rough token count for the prefix, at four characters each.

        Rough on purpose: the exact number depends on the tokenizer and
        the provider, and the decision it feeds is a coarse one — "is
        this anywhere near the minimum, or nowhere near it?"
        """
        return len(self.cacheable_prefix) // 4

    @property
    def cacheable_prefix(self) -> str:
        """Exactly the bytes a provider may cache.

        Exposed so the prefix-stability test can compare two independent
        builds without reaching into private state — the property being
        tested is that these bytes do not move, and a test that reads
        `_stable` would keep passing if the join changed.
        """
        return "\n\n".join(block.content for block in self._stable)
