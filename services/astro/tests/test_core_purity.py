"""Structural guard: `app/core` is a pure function library.

No database, no HTTP, no ambient clock. "Now" is passed in, never read.

That purity is not an aesthetic preference — it is what makes golden-file
testing possible at all. A function that reads the clock cannot have a
frozen expected output, and a function that opens a socket cannot be
replayed years later to explain a reading somebody was given.

The spec states this as task 2.5: "core cannot import httpx/asyncpg/
datetime.now". Prose does not enforce it. This does.

Sibling of tests/test_no_llm_imports.py, which guards the same service
against a different failure. If a change makes this test fail, the fix is
almost never to relax it — it is to move that work into `app/api/`, which
is the adapter layer and is allowed to do I/O.
"""

from __future__ import annotations

import ast
from pathlib import Path

CORE_DIR = Path(__file__).resolve().parent.parent / "app" / "core"

#: Modules `core` may not import, and why each one matters.
#:
#: Matched as dotted prefixes so "sqlalchemy.orm" is caught by
#: "sqlalchemy" without a bare-root check conflating unrelated packages.
BANNED_IMPORTS = {
    # I/O of any kind destroys reproducibility.
    "httpx": "network",
    "requests": "network",
    "aiohttp": "network",
    "urllib": "network",
    "socket": "network",
    # Persistence belongs to api-service. astro-service has no database
    # access at all — that is invariant 2 in the constitution.
    "asyncpg": "database",
    "psycopg": "database",
    "psycopg2": "database",
    "sqlalchemy": "database",
    "redis": "database",
    # The API layer is the adapter. core must not depend on its caller,
    # or the dependency graph acquires a cycle and core stops being
    # independently testable.
    "app.api": "layering",
    "fastapi": "layering",
}

#: Calls that read an ambient clock.
#:
#: This is the subtle one. `datetime.now()` inside a calculation makes
#: the output depend on when it ran, which is invisible in a unit test
#: written the same day and catastrophic in a golden file.
BANNED_CALLS = {
    ("datetime", "now"),
    ("datetime", "today"),
    ("datetime", "utcnow"),
    ("date", "today"),
    ("time", "time"),
}


def core_modules() -> list[Path]:
    files = sorted(CORE_DIR.rglob("*.py"))
    assert files, f"no modules found under {CORE_DIR} — this test would pass vacuously"
    return files


def test_core_imports_nothing_that_does_io() -> None:
    violations: list[str] = []

    for path in core_modules():
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))

        for node in ast.walk(tree):
            names: list[str] = []
            if isinstance(node, ast.Import):
                names = [alias.name for alias in node.names]
            elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
                names = [node.module]

            for name in names:
                for banned, reason in BANNED_IMPORTS.items():
                    if name == banned or name.startswith(banned + "."):
                        violations.append(f"{path.name}:{node.lineno} imports {name!r} ({reason})")

    assert not violations, (
        "app/core must stay pure — no I/O, no persistence, no dependency on "
        "its own caller. Move this into app/api/ instead:\n  " + "\n  ".join(violations)
    )


def test_core_never_reads_the_clock() -> None:
    """The instant is always an argument.

    `core` may import `datetime` — it needs the type — but may not call
    anything that asks the operating system what time it is.
    """
    violations: list[str] = []

    for path in core_modules():
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))

        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            func = node.func
            if not isinstance(func, ast.Attribute) or not isinstance(func.value, ast.Name):
                continue
            if (func.value.id, func.attr) in BANNED_CALLS:
                violations.append(f"{path.name}:{node.lineno} calls {func.value.id}.{func.attr}()")

    assert not violations, (
        "app/core must never read an ambient clock — 'now' is passed in. A "
        "calculation whose output depends on when it ran cannot have a "
        "golden file:\n  " + "\n  ".join(violations)
    )


def test_the_guard_can_actually_fire() -> None:
    """The guard, applied to a module that violates it.

    Without this the two tests above would pass on an empty directory, a
    typo in the path, or an AST walk that silently matches nothing. This
    project has already shipped two tests that asserted nothing; the
    cheapest defence is to prove the detector detects.
    """
    offending = ast.parse(
        "import httpx\nfrom datetime import datetime\ndef f():\n    return datetime.now()\n"
    )

    imports = [
        alias.name
        for node in ast.walk(offending)
        if isinstance(node, ast.Import)
        for alias in node.names
    ]
    assert any(name in BANNED_IMPORTS for name in imports), "the import check matches nothing"

    calls = [
        (node.func.value.id, node.func.attr)
        for node in ast.walk(offending)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and isinstance(node.func.value, ast.Name)
    ]
    assert any(call in BANNED_CALLS for call in calls), "the clock check matches nothing"
