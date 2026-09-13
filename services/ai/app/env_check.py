"""Catch typo'd environment variables at startup.

Pydantic's `extra="forbid"` is often described as making a mistyped
setting an error. It does — but only for entries in a `.env` file.
Environment variables that match no field are silently ignored, because
the whole OS environment would otherwise be "extra".

That is exactly backwards from where the protection is needed:
`.env` files are a local-development convenience, while production
config arrives as environment variables from Kubernetes, systemd or a
container runtime. So the one place a typo is most expensive is the one
place Pydantic does not look.

    DEFAULT_AYANMSA=lahiri   # note the missing 'A'

silently leaves `default_ayanamsa` at its default. For a setting that
selects the ayanamsa, that means every chart in the system is computed
against the wrong zodiac — and nothing anywhere reports a problem.

This module closes that gap by looking for environment variables whose
names are near-misses for a declared field.

NOTE: duplicated verbatim in services/astro — see app/observability.py for
why the two services keep their own copies.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from pydantic_settings import BaseSettings

# Maximum edit distance at which a name is treated as a typo rather than
# an unrelated variable. 2 catches a dropped letter, a transposition or a
# single substitution; going higher starts matching genuinely different
# names.
_MAX_DISTANCE = 2

# Below this length, short names collide by coincidence (PATH vs PATHS,
# ENV vs END). Only check names long enough for a near-miss to be
# meaningful.
_MIN_LENGTH = 6


class SuspectedTypoError(RuntimeError):
    """An environment variable looks like a misspelled setting."""


def _edit_distance(a: str, b: str, limit: int) -> int:
    """Levenshtein distance, abandoned once it exceeds `limit`."""
    if abs(len(a) - len(b)) > limit:
        return limit + 1

    previous = list(range(len(b) + 1))
    for i, ca in enumerate(a, start=1):
        current = [i]
        for j, cb in enumerate(b, start=1):
            current.append(
                min(
                    previous[j] + 1,  # deletion
                    current[j - 1] + 1,  # insertion
                    previous[j - 1] + (ca != cb),  # substitution
                )
            )
        if min(current) > limit:
            return limit + 1
        previous = current
    return previous[-1]


def find_suspected_typos(settings: BaseSettings) -> list[tuple[str, str]]:
    """Return (env_var, probable_intended_field) pairs.

    Pure and side-effect free so it can be tested without manipulating
    the real environment.
    """
    declared = {name.upper() for name in type(settings).model_fields}

    suspects: list[tuple[str, str]] = []
    for name in os.environ:
        upper = name.upper()
        if upper in declared or len(upper) < _MIN_LENGTH:
            continue

        best: tuple[int, str] | None = None
        for field in declared:
            if len(field) < _MIN_LENGTH:
                continue
            d = _edit_distance(upper, field, _MAX_DISTANCE)
            if d <= _MAX_DISTANCE and (best is None or d < best[0]):
                best = (d, field)

        if best is not None:
            suspects.append((name, best[1]))

    return sorted(suspects)


def assert_no_typos(settings: BaseSettings) -> None:
    """Raise if any environment variable looks like a misspelled setting.

    Fails rather than warns. A warning in a container log is read by
    nobody, and the failure mode being guarded against — silently running
    on a default while believing you configured something — is precisely
    the kind that goes unnoticed for months.
    """
    suspects = find_suspected_typos(settings)
    if not suspects:
        return

    lines = "\n".join(f"  {var}  — did you mean {field}?" for var, field in suspects)
    raise SuspectedTypoError(
        "Refusing to start: environment variable(s) look like misspelled settings.\n"
        f"{lines}\n\n"
        "A variable matching no field is ignored, so the setting you meant to "
        "change is silently sitting at its default. If the name is correct and "
        "unrelated to this service, rename it or unset it for this process."
    )
