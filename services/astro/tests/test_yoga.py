"""Aspects and yoga detection.

Every yoga gets a chart that satisfies it and one that NARROWLY does not
— one house out, one degree out, one planet substituted. The spec asks
for exactly that, and the reason is blunt: a predicate that has only ever
been seen returning True is indistinguishable from `return True`.

Charts here are SYNTHETIC. A yoga is a pure function of a chart, so
building the chart directly is both faster and far more precise than
hunting for a real birth date that happens to produce the configuration —
and it is the only way to construct the near-miss cases at all.
"""

from __future__ import annotations

import pytest

from app.core.aspects import aspected_house, aspects_house, aspects_of, mutual_aspect
from app.core.chart import Ascendant, House, PlacedPlanet, Rasi
from app.core.constants import (
    HOUSE_COUNT,
    NAKSHATRAS,
    SIGN_LORDS,
    SIGNS,
    Planet,
)
from app.core.yoga import (
    detect_all,
    detect_budhaditya,
    detect_chandra_mangal,
    detect_gajakesari,
    detect_kemadruma,
    detect_mahapurusha,
    detect_neecha_bhanga,
    detect_raja_yogas,
)


def build_chart(
    ascendant_sign: int = 0,
    placements: dict[Planet, int] | None = None,
    dignities: dict[Planet, str] | None = None,
    combust: set[Planet] | None = None,
) -> Rasi:
    """A synthetic Rasi with planets in chosen HOUSES.

    Houses rather than longitudes, because every yoga predicate reasons
    in houses. Longitudes are derived so the object is internally
    consistent — a planet's sign always matches the house it claims.

    Unlisted planets are parked in house 6, a house no yoga here treats
    as significant, so they cannot accidentally complete a combination
    the test did not intend.
    """
    placements = placements or {}
    dignities = dignities or {}
    combust = combust or set()

    planets: list[PlacedPlanet] = []
    for planet in Planet:
        house = placements.get(planet, 6)
        sign = (ascendant_sign + house - 1) % HOUSE_COUNT
        longitude = sign * 30.0 + 15.0

        planets.append(
            PlacedPlanet(
                planet=planet,
                longitude=longitude,
                sign=SIGNS[sign],
                sign_index=sign,
                degree=15.0,
                house=house,
                nakshatra=NAKSHATRAS[int(longitude // (360 / 27))],
                nakshatra_index=int(longitude // (360 / 27)),
                pada=1,
                is_retrograde=False,
                is_combust=planet in combust,
                dignity=dignities.get(planet, "neutral"),
                speed=1.0,
            )
        )

    houses = tuple(
        House(
            house=number,
            sign=SIGNS[(ascendant_sign + number - 1) % HOUSE_COUNT],
            sign_index=(ascendant_sign + number - 1) % HOUSE_COUNT,
            lord=SIGN_LORDS[(ascendant_sign + number - 1) % HOUSE_COUNT],
            planets=tuple(p.planet for p in planets if p.house == number),
        )
        for number in range(1, HOUSE_COUNT + 1)
    )

    return Rasi(
        ascendant=Ascendant(
            longitude=ascendant_sign * 30.0,
            sign=SIGNS[ascendant_sign],
            sign_index=ascendant_sign,
            degree=0.0,
            nakshatra=NAKSHATRAS[0],
            pada=1,
        ),
        houses=houses,
        planets=tuple(planets),
        ayanamsa_value=24.0,
    )


# ─── aspects ─────────────────────────────────────────────────────────


def test_counting_is_inclusive() -> None:
    """The seventh aspect is six houses forward, not seven.

    Vedic counting includes the planet's own house as 1. An
    implementation written from Western habit lands one house short on
    every aspect — consistently, so the result still looks like a
    coherent pattern.
    """
    assert aspected_house(1, 7) == 7
    assert aspected_house(1, 1) == 1
    assert aspected_house(10, 7) == 4  # wraps
    assert aspected_house(12, 2) == 1


def test_every_graha_aspects_the_seventh() -> None:
    for planet in Planet:
        houses = [a.house for a in aspects_of(planet, 1)]
        assert 7 in houses, f"{planet} does not aspect the seventh"


def test_the_special_drishtis() -> None:
    """Mars 4/8, Jupiter 5/9, Saturn 3/10 — and nobody else.

    Asserted exactly, not as a superset: a planet with extra aspects
    would produce yogas that should not be there, which is the failure
    direction that matters.
    """
    assert {a.house for a in aspects_of(Planet.MARS, 1)} == {4, 7, 8}
    assert {a.house for a in aspects_of(Planet.JUPITER, 1)} == {5, 7, 9}
    assert {a.house for a in aspects_of(Planet.SATURN, 1)} == {3, 7, 10}

    for ordinary in (Planet.SUN, Planet.MOON, Planet.VENUS, Planet.MERCURY, Planet.RAHU):
        assert {a.house for a in aspects_of(ordinary, 1)} == {7}


def test_mutual_aspect_requires_both_directions() -> None:
    """Special drishtis are not symmetric.

    Saturn in house 1 aspects house 10, but a planet in house 10 aspects
    house 4 — not house 1. Only the universal seventh is symmetric, and
    treating any aspect as mutual would invent Raja yogas.
    """
    assert aspects_house(Planet.SATURN, 1, 10)
    assert not aspects_house(Planet.VENUS, 10, 1)
    assert not mutual_aspect(Planet.SATURN, 1, Planet.VENUS, 10)

    # The seventh, which is symmetric.
    assert mutual_aspect(Planet.SATURN, 1, Planet.VENUS, 7)


def test_an_invalid_house_is_refused() -> None:
    for house in (0, 13, -1):
        with pytest.raises(ValueError, match="house must be"):
            aspected_house(house, 7)


# ─── Gajakesari ──────────────────────────────────────────────────────


def test_gajakesari_fires_when_jupiter_is_in_a_kendra_from_the_moon() -> None:
    for distance, jupiter_house in ((1, 1), (4, 4), (7, 7), (10, 10)):
        chart = build_chart(placements={Planet.MOON: 1, Planet.JUPITER: jupiter_house})
        assert detect_gajakesari(chart) is not None, f"kendra {distance} missed"


def test_gajakesari_does_not_fire_one_house_out() -> None:
    """The narrow miss.

    House 5 from the Moon is a trikona, not a kendra — adjacent to a
    genuine hit and exactly where an off-by-one lands.
    """
    for jupiter_house in (2, 3, 5, 6, 8, 9, 11, 12):
        chart = build_chart(placements={Planet.MOON: 1, Planet.JUPITER: jupiter_house})
        assert detect_gajakesari(chart) is None, f"fired with Jupiter in house {jupiter_house}"


def test_gajakesari_is_measured_from_the_moon_not_the_ascendant() -> None:
    """The commonest implementation error.

    Moon in house 2, Jupiter in house 4: Jupiter IS in a kendra from the
    ascendant, but is the 3rd from the Moon — so the yoga must not fire.
    A from-the-ascendant implementation passes every other test in this
    file.
    """
    chart = build_chart(placements={Planet.MOON: 2, Planet.JUPITER: 4})
    assert detect_gajakesari(chart) is None

    # And the converse: a kendra from the Moon but not from the ascendant.
    chart = build_chart(placements={Planet.MOON: 2, Planet.JUPITER: 5})
    assert detect_gajakesari(chart) is not None


# ─── Panch Mahapurusha ───────────────────────────────────────────────


def test_each_mahapurusha_yoga_fires() -> None:
    expected = {
        Planet.MARS: "Ruchaka Yoga",
        Planet.MERCURY: "Bhadra Yoga",
        Planet.JUPITER: "Hamsa Yoga",
        Planet.VENUS: "Malavya Yoga",
        Planet.SATURN: "Sasa Yoga",
    }

    for planet, name in expected.items():
        chart = build_chart(placements={planet: 4}, dignities={planet: "exalted"})
        names = [y.name for y in detect_mahapurusha(chart)]
        assert name in names, f"{name} missed"


def test_mahapurusha_needs_both_conditions() -> None:
    """Dignity alone is not enough, and a kendra alone is not enough.

    Either condition on its own is common; together they are not, which
    is the entire content of the combination. An implementation checking
    only one would fire for a large fraction of all charts.
    """
    # Exalted, but in house 6 — not a kendra.
    chart = build_chart(placements={Planet.MARS: 6}, dignities={Planet.MARS: "exalted"})
    assert detect_mahapurusha(chart) == []

    # In a kendra, but merely neutral.
    chart = build_chart(placements={Planet.MARS: 4}, dignities={Planet.MARS: "neutral"})
    assert detect_mahapurusha(chart) == []


def test_the_luminaries_form_no_mahapurusha_yoga() -> None:
    """These are the five great-person yogas of the true planets.

    The Sun and Moon have their own separate combinations; including them
    here would invent two yogas that do not exist.
    """
    for luminary in (Planet.SUN, Planet.MOON):
        chart = build_chart(placements={luminary: 1}, dignities={luminary: "exalted"})
        assert detect_mahapurusha(chart) == []


# ─── Budhaditya ──────────────────────────────────────────────────────


def test_budhaditya_fires_when_sun_and_mercury_share_a_sign() -> None:
    chart = build_chart(placements={Planet.SUN: 3, Planet.MERCURY: 3})
    assert detect_budhaditya(chart) is not None


def test_budhaditya_does_not_fire_in_adjacent_signs() -> None:
    chart = build_chart(placements={Planet.SUN: 3, Planet.MERCURY: 4})
    assert detect_budhaditya(chart) is None


def test_budhaditya_excludes_a_combust_mercury() -> None:
    """The narrow miss that matters most here.

    Mercury never strays more than ~28° from the Sun, so without this
    exclusion the yoga fires for nearly every chart where the two share a
    sign — which is most of them. A yoga that fires almost always carries
    no information.
    """
    chart = build_chart(placements={Planet.SUN: 3, Planet.MERCURY: 3}, combust={Planet.MERCURY})
    assert detect_budhaditya(chart) is None


# ─── Kemadruma ───────────────────────────────────────────────────────


def test_kemadruma_fires_when_the_moon_is_isolated() -> None:
    """Moon in house 1; everything else parked far away in house 6.

    Houses 12, 1 and 2 must all be empty of the counted planets.
    """
    chart = build_chart(placements={Planet.MOON: 1})
    assert detect_kemadruma(chart) is not None


def test_kemadruma_is_cancelled_by_a_neighbour() -> None:
    """The negative case that protects a person from a false affliction.

    This is the one yoga here that reports something unwelcome, so its
    false-positive direction is the dangerous one: claiming it wrongly
    tells someone their life is harder than it is. Each of the three
    neighbouring positions is checked separately.
    """
    moon_house = 5
    for occupied, description in (
        (4, "12th from the Moon"),
        (6, "2nd from the Moon"),
        (5, "with the Moon"),
    ):
        chart = build_chart(placements={Planet.MOON: moon_house, Planet.VENUS: occupied})
        assert detect_kemadruma(chart) is None, f"not cancelled by a planet {description}"


def test_kemadruma_ignores_the_sun_and_the_nodes() -> None:
    """Per the common formulation.

    Including them would cancel the yoga far more often than the
    tradition does, which is the same "fires almost never" failure as
    Budhaditya's inverse.
    """
    for ignored in (Planet.SUN, Planet.RAHU, Planet.KETU):
        chart = build_chart(placements={Planet.MOON: 1, ignored: 2})
        assert detect_kemadruma(chart) is not None, f"{ignored} wrongly cancelled it"


# ─── Chandra-Mangal ──────────────────────────────────────────────────


def test_chandra_mangal_fires_on_conjunction_and_on_opposition() -> None:
    conjunct = build_chart(placements={Planet.MOON: 3, Planet.MARS: 3})
    assert detect_chandra_mangal(conjunct) is not None
    assert detect_chandra_mangal(conjunct).strength == "strong"

    # Seventh from each other — mutual, because the seventh is symmetric.
    opposed = build_chart(placements={Planet.MOON: 1, Planet.MARS: 7})
    assert detect_chandra_mangal(opposed) is not None
    assert detect_chandra_mangal(opposed).strength == "moderate"


def test_chandra_mangal_does_not_fire_on_a_one_way_aspect() -> None:
    """Mars aspects the 4th and 8th; the Moon does not.

    Moon in 1, Mars in 10: Mars aspects house 1 via its fourth drishti,
    but the Moon only aspects house 7. Not mutual, so no yoga — and an
    implementation that checked either direction would fire here.
    """
    chart = build_chart(placements={Planet.MOON: 1, Planet.MARS: 10})
    assert aspects_house(Planet.MARS, 10, 1)
    assert not aspects_house(Planet.MOON, 1, 10)
    assert detect_chandra_mangal(chart) is None


# ─── Neecha Bhanga ───────────────────────────────────────────────────


def test_neecha_bhanga_fires_when_the_dispositor_sits_in_a_kendra() -> None:
    """Aries ascendant, a debilitated Sun in Libra (house 7).

    Libra's lord is Venus; placing Venus in house 4 — a kendra from the
    ascendant — cancels the debilitation.
    """
    chart = build_chart(
        ascendant_sign=0,
        placements={Planet.SUN: 7, Planet.VENUS: 4},
        dignities={Planet.SUN: "debilitated"},
    )
    found = detect_neecha_bhanga(chart)
    assert len(found) == 1
    assert Planet.SUN in found[0].involved_planets


def test_neecha_bhanga_fires_from_a_kendra_from_the_moon() -> None:
    """The second condition, which has to be tested separately.

    Venus in house 3 is NOT a kendra from an Aries ascendant, but with
    the Moon in house 6 it is the 10th from the Moon — a kendra — so the
    cancellation stands.

    This case was found by the negative test below failing: I had placed
    the dispositor off a kendra from the ascendant and forgotten the Moon
    sitting at the builder's default of house 6. The code was right and
    the test was under-specified.
    """
    chart = build_chart(
        ascendant_sign=0,
        placements={Planet.SUN: 7, Planet.VENUS: 3, Planet.MOON: 6},
        dignities={Planet.SUN: "debilitated"},
    )
    assert len(detect_neecha_bhanga(chart)) == 1


def test_neecha_bhanga_does_not_fire_from_a_non_kendra() -> None:
    """The narrow miss: the dispositor off a kendra from BOTH references.

    Venus in house 3, Moon in house 2. House 3 is not a kendra from the
    Aries ascendant, and is the 2nd from the Moon — also not a kendra. So
    neither condition holds and the debilitation stands uncancelled.
    """
    chart = build_chart(
        ascendant_sign=0,
        placements={Planet.SUN: 7, Planet.VENUS: 3, Planet.MOON: 2},
        dignities={Planet.SUN: "debilitated"},
    )
    assert detect_neecha_bhanga(chart) == []


def test_neecha_bhanga_needs_an_actual_debilitation() -> None:
    chart = build_chart(
        ascendant_sign=0,
        placements={Planet.SUN: 7, Planet.VENUS: 4},
        dignities={Planet.SUN: "neutral"},
    )
    assert detect_neecha_bhanga(chart) == []


# ─── Raja yoga ───────────────────────────────────────────────────────


def test_raja_yoga_fires_for_a_kendra_and_trikona_lord_conjunction() -> None:
    """Aries ascendant: house 4 is Cancer (Moon), house 5 is Leo (Sun).

    Moon is a kendra lord, Sun a trikona lord. Placing them together
    forms the yoga.
    """
    chart = build_chart(ascendant_sign=0, placements={Planet.MOON: 2, Planet.SUN: 2})
    found = detect_raja_yogas(chart)
    assert found, "no Raja yoga for a kendra-trikona lord conjunction"
    assert any({Planet.MOON, Planet.SUN} <= set(y.involved_planets) for y in found)


def test_raja_yoga_never_fires_from_a_lord_pairing_with_itself() -> None:
    """House 1 is BOTH a kendra and a trikona.

    Without an explicit exclusion its lord pairs with itself, every chart
    ever cast reports a Raja yoga, and the detection is worthless. The
    single most important negative test in this file.
    """
    # Every planet scattered so no two share a house and none aspect
    # mutually — yet house 1's lord is trivially "conjunct itself".
    chart = build_chart(
        ascendant_sign=0,
        placements={
            Planet.MARS: 1,
            Planet.VENUS: 2,
            Planet.MERCURY: 3,
            Planet.MOON: 12,
            Planet.SUN: 5,
            Planet.JUPITER: 6,
            Planet.SATURN: 8,
            Planet.RAHU: 9,
            Planet.KETU: 3,
        },
    )
    for yoga in detect_raja_yogas(chart):
        first, second = yoga.involved_planets
        assert first != second, f"{first} formed a Raja yoga with itself"


def test_raja_yoga_does_not_duplicate_the_same_pair() -> None:
    """One pair can qualify via several kendra/trikona combinations.

    Reporting it four times would inflate the yoga list and, downstream,
    make a chart look far more fortunate than it is.
    """
    chart = build_chart(ascendant_sign=0, placements={Planet.MOON: 2, Planet.SUN: 2})
    pairs = [tuple(sorted(y.involved_planets)) for y in detect_raja_yogas(chart)]
    assert len(pairs) == len(set(pairs)), f"duplicate pairs: {pairs}"


# ─── the whole set ───────────────────────────────────────────────────


def test_detect_all_reports_nothing_without_a_birth_time() -> None:
    """Every yoga here depends on houses, and houses need an ascendant.

    Reporting yogas from a chart with no birth time would be exactly the
    fabrication the unknown-time contract exists to prevent.
    """
    timeless = Rasi(ascendant=None, houses=None, planets=build_chart().planets, ayanamsa_value=24.0)
    assert detect_all(timeless) == ()


def test_at_least_ten_distinct_yogas_are_implemented() -> None:
    """The spec's gate item: "≥10 yogas detected"."""
    from app.core.yoga import MAHAPURUSHA

    named = {
        "Gajakesari Yoga",
        "Budhaditya Yoga",
        "Kemadruma Yoga",
        "Chandra-Mangal Yoga",
        "Neecha Bhanga Raja Yoga",
        "Raja Yoga",
        *MAHAPURUSHA.values(),
    }
    assert len(named) >= 10, f"only {len(named)} yogas: {sorted(named)}"


def test_a_real_chart_produces_plausible_yogas(ephemeris, ayanamsa, timescale) -> None:
    """An end-to-end sanity pass on a real chart.

    Not an assertion about which yogas a 1994 Jaipur birth should have —
    that would need a reference this project does not have yet. It checks
    that detection runs against real ephemeris output and returns
    well-formed results, which the synthetic tests cannot: they only ever
    see charts this file built.
    """
    from app.core.chart import compute_rasi

    rasi = compute_rasi(ephemeris, ayanamsa, timescale.utc(1994, 8, 17, 9, 5), 26.9124, 75.7873)
    yogas = detect_all(rasi)

    for yoga in yogas:
        assert yoga.name
        assert yoga.strength in ("strong", "moderate")
        assert yoga.involved_planets
        assert all(1 <= h <= HOUSE_COUNT for h in yoga.involved_houses)

    names = [y.name for y in yogas]
    assert len(names) == len(set(names)) or "Raja Yoga" in names
