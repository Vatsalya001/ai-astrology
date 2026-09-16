"""Graha drishti — planetary aspects.

Pure functions over a chart.

── Counting is inclusive, and that is the whole difficulty ──

Vedic aspects are counted inclusively from the planet's own house: the
planet's house is 1, the next is 2, and "the seventh aspect" means six
houses forward. Western convention counts exclusively, so an
implementation written from Western habit lands one house short on every
aspect in the chart — consistently, which means the result still looks
like a coherent aspect pattern.
"""

from __future__ import annotations

from dataclasses import dataclass

from app.core.constants import HOUSE_COUNT, SPECIAL_ASPECTS, UNIVERSAL_ASPECT, Planet


@dataclass(frozen=True, slots=True)
class Aspect:
    """One planet aspecting one house."""

    planet: Planet
    house: int
    """The house aspected, 1-12."""

    distance: int
    """Which drishti this is — 7 for the universal one, 4/8 for Mars,
    5/9 for Jupiter, 3/10 for Saturn."""


def aspected_house(from_house: int, distance: int) -> int:
    """The house `distance` places away, counted inclusively.

    `distance=7` from house 1 gives house 7, not house 8: the planet's
    own house counts as the first. Hence the `- 1` before the modulo.
    """
    if not 1 <= from_house <= HOUSE_COUNT:
        raise ValueError(f"house must be 1-12, got {from_house}")
    return (from_house - 1 + distance - 1) % HOUSE_COUNT + 1


def aspects_of(planet: Planet, house: int) -> tuple[Aspect, ...]:
    """Every house a planet aspects from a given house.

    All nine grahas aspect the seventh. Mars, Jupiter and Saturn have
    additional special drishtis; the nodes and the luminaries have only
    the seventh.
    """
    distances = (UNIVERSAL_ASPECT, *SPECIAL_ASPECTS.get(planet, ()))

    return tuple(
        Aspect(planet=planet, house=aspected_house(house, distance), distance=distance)
        for distance in sorted(distances)
    )


def all_aspects(placements: dict[Planet, int]) -> tuple[Aspect, ...]:
    """Every aspect cast in a chart, given each planet's house."""
    return tuple(
        aspect for planet, house in placements.items() for aspect in aspects_of(planet, house)
    )


def aspects_house(planet: Planet, from_house: int, target: int) -> bool:
    """Does `planet` in `from_house` aspect `target`?"""
    return any(aspect.house == target for aspect in aspects_of(planet, from_house))


def mutual_aspect(first: Planet, first_house: int, second: Planet, second_house: int) -> bool:
    """Do two planets aspect each other?

    Mutual means BOTH directions, which is not automatic once special
    drishtis are involved: Saturn in house 1 aspects house 10, but a
    planet in house 10 aspects house 4, not house 1. Only the universal
    seventh aspect is symmetric.
    """
    return aspects_house(first, first_house, second_house) and aspects_house(
        second, second_house, first_house
    )
