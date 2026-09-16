"""Planetary positions.

Pure with respect to the outside world: no clock, no network, no
database. The instant is passed in; the JPL kernel is opened once, from
a local path, by whoever constructs the provider.

── Why the kernel is vendored rather than downloaded ──

`skyfield` will happily fetch its own kernels. It must not, for two
reasons.

The first is structural: `tests/test_no_llm_imports.py` bans HTTP clients
from this service outright, and says why — "today it fetches a timezone
database, tomorrow someone points it at a model endpoint". A service that
phones home on boot is also not deterministic, which is the property this
entire phase exists to provide.

The second is that the download is not reliable. The canonical JPL URL
for `de421.bsp` returns 404 today; the file now lives under
`a_old_versions/`. A build that depends on a third-party URL which has
already moved once is a build that breaks on someone else's schedule.

de421 covers 1899-07-29 to 2053-10-09, which spans every era the golden
fixtures exercise, including the pre-1906 Indian local-mean-time case.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from skyfield.api import load_file
from skyfield.jpllib import SpiceKernel
from skyfield.positionlib import Barycentric
from skyfield.timelib import Time, Timescale

from app.core.constants import BODIES, NODES, Planet, normalise_longitude

#: Skyfield's name for each graha's target in the kernel.
#:
#: Barycentres for the outer planets: de421 carries Mercury, Venus and
#: Mars as both barycentre and body, but Jupiter through Saturn only as
#: barycentres. The difference is far below arcsecond level at Earth
#: distance and irrelevant at the precision astrology works in.
_KERNEL_TARGETS = {
    Planet.SUN: "sun",
    Planet.MOON: "moon",
    Planet.MERCURY: "mercury barycenter",
    Planet.VENUS: "venus barycenter",
    Planet.MARS: "mars barycenter",
    Planet.JUPITER: "jupiter barycenter",
    Planet.SATURN: "saturn barycenter",
}


@dataclass(frozen=True, slots=True)
class Position:
    """Where a graha is, tropically.

    Tropical rather than sidereal because the ayanamsa is a separate,
    configurable concern — baking it in here would make the provider
    depend on a user preference.
    """

    planet: Planet
    longitude: float
    """Tropical ecliptic longitude of date, in [0, 360)."""

    latitude: float
    speed: float
    """Degrees per day. Negative means retrograde."""

    @property
    def is_retrograde(self) -> bool:
        """Apparent backward motion.

        Derived from the sign of the speed rather than stored, so the two
        can never disagree. The nodes override this — they are always
        retrograde, and have no observed speed to take a sign from.
        """
        return self.speed < 0


class EphemerisProvider(Protocol):
    """The seam ADR-003 called the exit.

    `EPHEMERIS_PROVIDER` is a real configuration value with a real
    interface behind it. If cross-validation ever exposes something about
    `skyfield` we cannot reconcile, swapping the implementation is a
    constructor change rather than a rewrite — which is what makes that
    ADR reversible rather than merely optimistic.
    """

    def positions(self, t: Time) -> dict[Planet, Position]:
        """Tropical positions for all nine grahas at an instant."""
        ...

    def position_of(self, planet: Planet, t: Time) -> Position:
        """One graha, without computing the others."""
        ...

    def longitude_of(self, planet: Planet, t: Time) -> float:
        """Just the tropical longitude, without deriving speed."""
        ...


class SkyfieldEphemeris:
    """Positions from a JPL kernel via skyfield."""

    def __init__(self, kernel_path: Path, timescale: Timescale) -> None:
        if not kernel_path.exists():
            # Loud and actionable. The alternative — skyfield silently
            # downloading — is the thing this service must never do.
            raise FileNotFoundError(
                f"ephemeris kernel missing at {kernel_path}. "
                "Run `task astro:ephemeris` to vendor it. This service "
                "never downloads at runtime; see app/core/ephemeris.py."
            )

        self._kernel: SpiceKernel = load_file(str(kernel_path))
        self._earth = self._kernel["earth"]
        self._timescale = timescale

    @property
    def kernel(self) -> SpiceKernel:
        """The loaded kernel, for callers that need their own geometry."""
        return self._kernel

    def longitude_of(self, planet: Planet, t: Time) -> float:
        """Just the tropical longitude — no speed, no latitude.

        `position_of` derives daily motion by a central difference, which
        costs two EXTRA ephemeris evaluations. The Sade Sati scan walks
        decades asking only which sign Saturn is in, and never looks at
        its speed, so paying for that is three times the work for nothing.
        """
        if planet in NODES:
            rahu, ketu = self._nodes(t)
            return rahu.longitude if planet is Planet.RAHU else ketu.longitude
        return normalise_longitude(self._tropical_longitude(planet, t))

    def position_of(self, planet: Planet, t: Time) -> Position:
        """One graha, without computing the other eight.

        The Sade Sati scan walks decades in five-day steps looking only
        at Saturn. Going through `positions` made that nine times more
        expensive than it needed to be — six minutes for the transit
        suite — and the spec budgets 200ms for a whole chart.
        """
        if planet in NODES:
            rahu, ketu = self._nodes(t)
            return rahu if planet is Planet.RAHU else ketu
        return self._observe(self._earth.at(t), planet, t)

    def positions(self, t: Time) -> dict[Planet, Position]:
        observer = self._earth.at(t)
        result: dict[Planet, Position] = {}

        for planet in BODIES:
            result[planet] = self._observe(observer, planet, t)

        rahu, ketu = self._nodes(t)
        result[Planet.RAHU] = rahu
        result[Planet.KETU] = ketu
        return result

    def _observe(self, observer: Barycentric, planet: Planet, t: Time) -> Position:
        target = self._kernel[_KERNEL_TARGETS[planet]]
        astrometric = observer.observe(target).apparent()

        # epoch=t → ecliptic OF DATE, not J2000. Omitting it produces
        # longitudes frozen in the year 2000, which are wrong by the
        # accumulated precession and wrong consistently, so every planet
        # shifts together and the chart still looks coherent.
        latitude, longitude, _ = astrometric.ecliptic_latlon(epoch=t)

        return Position(
            planet=planet,
            longitude=normalise_longitude(float(longitude.degrees)),
            latitude=float(latitude.degrees),
            speed=self._daily_motion(planet, t),
        )

    def _daily_motion(self, planet: Planet, t: Time) -> float:
        """Degrees per day, by central difference over ±6 hours.

        Skyfield does not expose an ecliptic-longitude rate directly, and
        this is what retrogradation is detected from, so it has to be
        derived rather than guessed.

        A symmetric difference rather than a forward one: forward
        differencing biases the estimate by half a step, which matters
        precisely at a station, where a planet's speed passes through
        zero and the retrograde flag flips.
        """
        half_step = 0.25  # days
        before = self._tropical_longitude(planet, self._timescale.tt_jd(t.tt - half_step))
        after = self._tropical_longitude(planet, self._timescale.tt_jd(t.tt + half_step))

        delta = after - before
        # Unwrap across the 0°/360° seam, which the Moon crosses monthly.
        if delta > 180.0:
            delta -= 360.0
        elif delta < -180.0:
            delta += 360.0

        return delta / (2 * half_step)

    def _tropical_longitude(self, planet: Planet, t: Time) -> float:
        target = self._kernel[_KERNEL_TARGETS[planet]]
        _, longitude, _ = self._earth.at(t).observe(target).apparent().ecliptic_latlon(epoch=t)
        return float(longitude.degrees)

    def _nodes(self, t: Time) -> tuple[Position, Position]:
        """Rahu and Ketu — the mean lunar nodes.

        Mean rather than true, which is the Vedic convention: the true
        node oscillates by up to 1.5° with a roughly fortnightly period,
        and classical technique is built on the smooth mean motion.

        The mean node is not a body in the kernel, so it comes from the
        standard polynomial in the Julian centuries since J2000. Ketu is
        exactly opposite by definition, never computed independently —
        a separate computation could drift and produce a chart where the
        nodes are not 180° apart, which is impossible.
        """
        centuries = (t.tt - 2451545.0) / 36525.0

        # Mean longitude of the ascending node, IAU 1980 nutation series.
        # Degrees. This is a tropical mean longitude of date.
        mean_node = (
            125.04452 - 1934.136261 * centuries + 0.0020708 * centuries**2 + centuries**3 / 450000.0
        )

        # Daily motion, from the derivative of the same polynomial.
        # Always negative: the nodes regress, which is why Rahu and Ketu
        # are always retrograde.
        daily = (-1934.136261 + 2 * 0.0020708 * centuries) / 36525.0

        rahu_longitude = normalise_longitude(mean_node)
        return (
            Position(Planet.RAHU, rahu_longitude, 0.0, daily),
            Position(Planet.KETU, normalise_longitude(rahu_longitude + 180.0), 0.0, daily),
        )
