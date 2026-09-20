"""The fact index: what is actually true about one person's chart.

── This is the automated half of invariant 1 ──

`.claude/CLAUDE.md`: "Astrology is computed, never generated." Topology
enforces the first half — `astro-service` has no model access, so a
model cannot compute a position. This file enforces the second half: a
model cannot *claim* one either. Every astrological statement a response
makes about the user is checked against the numbers `astro-service`
supplied, and a mismatch is a hard block.

Without this, the invariant holds right up until the model says "Saturn
in your 10th house" about a chart where Saturn is in the 4th, which it
will eventually do, fluently and with confidence.

── Personal claims only ──

"Saturn is traditionally associated with discipline" is a statement
about astrology. "Saturn is in your 10th house" is a statement about
*you*, and only the second is checkable. Conflating them would block
every general explanation the product exists to give.

The distinction is carried by the possessive, which is also how a reader
tells them apart — see `app/validation/claims.py`.
"""

from __future__ import annotations

from pydantic import BaseModel, Field

SIGNS = (
    "aries",
    "taurus",
    "gemini",
    "cancer",
    "leo",
    "virgo",
    "libra",
    "scorpio",
    "sagittarius",
    "capricorn",
    "aquarius",
    "pisces",
)

PLANETS = (
    "sun",
    "moon",
    "mars",
    "mercury",
    "jupiter",
    "venus",
    "saturn",
    "rahu",
    "ketu",
)

# Vedic synonyms. A response is as likely to say "Shani" as "Saturn",
# and an index that only knew the English name would treat a correct
# Sanskrit claim as unverifiable — which under the empty-index rule
# below means blocking a true statement.
ALIASES: dict[str, str] = {
    "surya": "sun",
    "ravi": "sun",
    "chandra": "moon",
    "soma": "moon",
    "mangal": "mars",
    "kuja": "mars",
    "budh": "mercury",
    "budha": "mercury",
    "guru": "jupiter",
    "brihaspati": "jupiter",
    "shukra": "venus",
    "sukra": "venus",
    "shani": "saturn",
    "sani": "saturn",
    "mesha": "aries",
    "vrishabha": "taurus",
    "mithuna": "gemini",
    "karka": "cancer",
    "simha": "leo",
    "kanya": "virgo",
    "tula": "libra",
    "vrischika": "scorpio",
    "dhanu": "sagittarius",
    "makara": "capricorn",
    "kumbha": "aquarius",
    "meena": "pisces",
}


def canonical(name: str) -> str:
    """One spelling per body, so comparison is not a spelling test."""
    lowered = name.strip().lower()
    return ALIASES.get(lowered, lowered)


class PlanetFact(BaseModel):
    """Where one body actually is, as computed upstream."""

    sign: str
    house: int = Field(ge=1, le=12)
    nakshatra: str = ""
    retrograde: bool = False

    def model_post_init(self, _: object) -> None:
        # Canonicalised on the way IN, so every comparison later is a
        # plain string equality. Normalising at comparison time instead
        # means every call site has to remember to, and one of them
        # will not.
        object.__setattr__(self, "sign", canonical(self.sign))
        object.__setattr__(self, "nakshatra", self.nakshatra.strip().lower())


class FactIndex(BaseModel):
    """Everything checkable about one chart.

    Supplied with the context by `astro-service` and carried through the
    request. Phase 5 fills it from the real chart; here it is the seam,
    which is what makes Phase 5 additive rather than a refactor.
    """

    planets: dict[str, PlanetFact] = Field(default_factory=dict)
    ascendant: str = ""
    moon_sign: str = ""
    sun_sign: str = ""
    current_dasha: str = ""
    current_antardasha: str = ""

    def model_post_init(self, _: object) -> None:
        object.__setattr__(
            self, "planets", {canonical(name): fact for name, fact in self.planets.items()}
        )
        for field in ("ascendant", "moon_sign", "sun_sign"):
            object.__setattr__(self, field, canonical(getattr(self, field)))
        for field in ("current_dasha", "current_antardasha"):
            object.__setattr__(self, field, canonical(getattr(self, field)))

    @property
    def is_empty(self) -> bool:
        """No chart was supplied at all.

        Read by the validator, and the branch it drives is deliberately
        the strict one: a response making a personal chart claim when no
        chart was given has invented the chart outright. That is the
        most serious version of the failure this module exists to
        catch, not the most forgivable.
        """
        return not self.planets and not self.ascendant

    def house_of(self, planet: str) -> int | None:
        fact = self.planets.get(canonical(planet))
        return fact.house if fact else None

    def sign_of(self, planet: str) -> str | None:
        fact = self.planets.get(canonical(planet))
        return fact.sign if fact else None

    def nakshatra_of(self, planet: str) -> str | None:
        fact = self.planets.get(canonical(planet))
        return fact.nakshatra if fact and fact.nakshatra else None

    def knows(self, planet: str) -> bool:
        return canonical(planet) in self.planets
