"""Layer 3: what the user sees is checked before they see it.

`fabricated_chart_fact` is the automated half of `.claude/CLAUDE.md`'s
first invariant. Topology stops `astro-service` computing with a model;
this stops a model claiming a computation it never did.
"""

from app.validation.claims import Claim, extract_claims
from app.validation.facts import ALIASES, PLANETS, SIGNS, FactIndex, PlanetFact, canonical
from app.validation.validator import OutputValidator, Severity, Violation, ViolationType

__all__ = [
    "ALIASES",
    "PLANETS",
    "SIGNS",
    "Claim",
    "FactIndex",
    "OutputValidator",
    "PlanetFact",
    "Severity",
    "Violation",
    "ViolationType",
    "canonical",
    "extract_claims",
]
