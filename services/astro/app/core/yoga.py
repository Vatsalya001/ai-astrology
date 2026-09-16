"""Yoga detection.

Each yoga is a pure predicate over a `Rasi`. Small, correct and
individually testable, rather than a large shaky set — the spec is
explicit about preferring the former, and so is the risk: a yoga is a
claim about someone's life, and a false positive is worse than a missing
one.

Every predicate here has a test that satisfies it and a test that
NARROWLY does not — one house out, one degree out, one planet
substituted. A yoga that only ever fires is indistinguishable from a
function that returns True.
"""

from __future__ import annotations

from dataclasses import dataclass

from app.core.aspects import mutual_aspect
from app.core.chart import PlacedPlanet, Rasi
from app.core.constants import HOUSE_COUNT, NODES, SIGN_LORDS, Planet

#: The angular houses. Strength in Vedic astrology concentrates here.
KENDRAS = (1, 4, 7, 10)

#: The trinal houses, associated with fortune and merit.
TRIKONAS = (1, 5, 9)

#: The five Panch Mahapurusha yogas, each formed by one planet.
#:
#: The luminaries are absent by definition — these are the "great person"
#: yogas of the five true planets, and the Sun and Moon have their own
#: separate combinations.
MAHAPURUSHA = {
    Planet.MARS: "Ruchaka Yoga",
    Planet.MERCURY: "Bhadra Yoga",
    Planet.JUPITER: "Hamsa Yoga",
    Planet.VENUS: "Malavya Yoga",
    Planet.SATURN: "Sasa Yoga",
}


@dataclass(frozen=True, slots=True)
class Yoga:
    """A detected combination."""

    name: str
    strength: str
    """strong | moderate — never a number. A numeric score would imply a
    precision the tradition does not have and the product cannot
    defend."""

    involved_planets: tuple[Planet, ...]
    involved_houses: tuple[int, ...]


def _by_planet(rasi: Rasi) -> dict[Planet, PlacedPlanet]:
    return {p.planet: p for p in rasi.planets}


def _house_distance(from_house: int, to_house: int) -> int:
    """Inclusive count from one house to another, 1-12."""
    return (to_house - from_house) % HOUSE_COUNT + 1


def _lord_of_house(rasi: Rasi, house: int) -> Planet:
    assert rasi.houses is not None
    return rasi.houses[house - 1].lord


def detect_gajakesari(rasi: Rasi) -> Yoga | None:
    """Jupiter in a kendra FROM THE MOON.

    From the Moon, not from the ascendant — the commonest error in
    implementations, and one that still produces plausible output because
    Jupiter is in a kendra from something about a third of the time
    either way.
    """
    planets = _by_planet(rasi)
    moon, jupiter = planets[Planet.MOON], planets[Planet.JUPITER]

    if _house_distance(moon.house, jupiter.house) not in KENDRAS:
        return None

    return Yoga(
        name="Gajakesari Yoga",
        strength="strong" if jupiter.dignity in ("exalted", "own_sign") else "moderate",
        involved_planets=(Planet.JUPITER, Planet.MOON),
        involved_houses=(jupiter.house, moon.house),
    )


def detect_mahapurusha(rasi: Rasi) -> list[Yoga]:
    """The five great-person yogas.

    Each needs BOTH conditions: the planet in its own sign or exaltation,
    AND in a kendra from the ascendant. Either alone is common; together
    they are not, which is the entire point of the combination.
    """
    found: list[Yoga] = []
    planets = _by_planet(rasi)

    for planet, name in MAHAPURUSHA.items():
        placed = planets[planet]
        if placed.house not in KENDRAS:
            continue
        if placed.dignity not in ("exalted", "own_sign", "moolatrikona"):
            continue

        found.append(
            Yoga(
                name=name,
                strength="strong" if placed.dignity == "exalted" else "moderate",
                involved_planets=(planet,),
                involved_houses=(placed.house,),
            )
        )

    return found


def detect_budhaditya(rasi: Rasi) -> Yoga | None:
    """Sun and Mercury in the same sign.

    Mercury never strays more than about 28° from the Sun, so this is
    common — it is reported as moderate, never strong, and the caveat
    belongs in the interpretation layer rather than here.

    Combust Mercury is excluded. The traditional reading is that a
    scorched Mercury cannot deliver the yoga's benefit, and including it
    would make the yoga fire for nearly every chart where the two share a
    sign, which is most of them.
    """
    planets = _by_planet(rasi)
    sun, mercury = planets[Planet.SUN], planets[Planet.MERCURY]

    if sun.sign_index != mercury.sign_index or mercury.is_combust:
        return None

    return Yoga(
        name="Budhaditya Yoga",
        strength="moderate",
        involved_planets=(Planet.SUN, Planet.MERCURY),
        involved_houses=(sun.house,),
    )


def detect_kemadruma(rasi: Rasi) -> Yoga | None:
    """The Moon isolated: nothing in the 2nd or 12th from it, and nothing with it.

    An affliction rather than a blessing, and the one yoga here that
    reports something unwelcome — which is exactly why its negative test
    matters more than its positive one. Claiming this wrongly is telling
    someone their life is harder than it is.

    The Sun and the nodes are excluded from the count, per the common
    formulation.
    """
    planets = _by_planet(rasi)
    moon = planets[Planet.MOON]

    neighbours = {
        (moon.house - 2) % HOUSE_COUNT + 1,  # 12th from the Moon
        moon.house % HOUSE_COUNT + 1,  # 2nd from the Moon
        moon.house,  # with the Moon
    }

    for placed in rasi.planets:
        if placed.planet in (Planet.MOON, Planet.SUN) or placed.planet in NODES:
            continue
        if placed.house in neighbours:
            return None

    return Yoga(
        name="Kemadruma Yoga",
        strength="moderate",
        involved_planets=(Planet.MOON,),
        involved_houses=(moon.house,),
    )


def detect_chandra_mangal(rasi: Rasi) -> Yoga | None:
    """Moon and Mars conjunct or in mutual aspect."""
    planets = _by_planet(rasi)
    moon, mars = planets[Planet.MOON], planets[Planet.MARS]

    conjunct = moon.house == mars.house
    aspecting = mutual_aspect(Planet.MOON, moon.house, Planet.MARS, mars.house)

    if not (conjunct or aspecting):
        return None

    return Yoga(
        name="Chandra-Mangal Yoga",
        strength="strong" if conjunct else "moderate",
        involved_planets=(Planet.MOON, Planet.MARS),
        involved_houses=(moon.house, mars.house),
    )


def detect_neecha_bhanga(rasi: Rasi) -> list[Yoga]:
    """Cancellation of debilitation.

    A debilitated planet's weakness is cancelled when the lord of the
    sign it sits in is itself in a kendra from the ascendant or from the
    Moon.

    Only one of the several traditional conditions is implemented, and
    deliberately so: the others disagree between authorities, and a
    permissive union of all of them would cancel nearly every
    debilitation, which makes the yoga meaningless. One clearly-stated
    rule that sometimes misses beats a broad one that always fires.
    """
    found: list[Yoga] = []
    planets = _by_planet(rasi)
    moon = planets[Planet.MOON]

    for placed in rasi.planets:
        if placed.dignity != "debilitated":
            continue

        dispositor = SIGN_LORDS[placed.sign_index]
        lord_placement = planets.get(dispositor)
        if lord_placement is None:
            continue

        from_ascendant = lord_placement.house in KENDRAS
        from_moon = _house_distance(moon.house, lord_placement.house) in KENDRAS

        if from_ascendant or from_moon:
            found.append(
                Yoga(
                    name="Neecha Bhanga Raja Yoga",
                    strength="moderate",
                    involved_planets=(placed.planet, dispositor),
                    involved_houses=(placed.house, lord_placement.house),
                )
            )

    return found


def detect_raja_yogas(rasi: Rasi) -> list[Yoga]:
    """A kendra lord and a trikona lord, conjunct or in mutual aspect.

    The first house is both a kendra and a trikona, so its lord can pair
    with itself — which would report a yoga for every chart ever cast.
    The same planet in both roles is therefore excluded explicitly.
    """
    if rasi.houses is None:
        return []

    found: list[Yoga] = []
    planets = _by_planet(rasi)
    seen: set[tuple[Planet, Planet]] = set()

    for kendra in KENDRAS:
        for trikona in TRIKONAS:
            kendra_lord = _lord_of_house(rasi, kendra)
            trikona_lord = _lord_of_house(rasi, trikona)

            if kendra_lord == trikona_lord:
                continue

            pair = tuple(sorted((kendra_lord, trikona_lord)))
            if pair in seen:
                continue

            first, second = planets[kendra_lord], planets[trikona_lord]
            conjunct = first.house == second.house
            aspecting = mutual_aspect(kendra_lord, first.house, trikona_lord, second.house)

            if not (conjunct or aspecting):
                continue

            seen.add(pair)  # type: ignore[arg-type]
            found.append(
                Yoga(
                    name="Raja Yoga",
                    strength="strong" if conjunct else "moderate",
                    involved_planets=(kendra_lord, trikona_lord),
                    involved_houses=(first.house, second.house),
                )
            )

    return found


def detect_all(rasi: Rasi) -> tuple[Yoga, ...]:
    """Every yoga this engine knows about.

    Returns nothing at all when the birth time is unknown. Every yoga
    here depends on house placement, and houses depend on the ascendant —
    so without a birth time there is nothing to detect, and reporting
    yogas anyway would be exactly the fabrication the unknown-time
    contract exists to prevent.
    """
    if rasi.houses is None:
        return ()

    found: list[Yoga] = []

    for single in (
        detect_gajakesari(rasi),
        detect_budhaditya(rasi),
        detect_kemadruma(rasi),
        detect_chandra_mangal(rasi),
    ):
        if single is not None:
            found.append(single)

    found.extend(detect_mahapurusha(rasi))
    found.extend(detect_neecha_bhanga(rasi))
    found.extend(detect_raja_yogas(rasi))

    return tuple(found)
