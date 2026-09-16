"""Ayanamsa — the tropical-to-sidereal offset.

`sidereal = tropical - ayanamsa`. Everything downstream depends on this
number, and an error here is invisible: it shifts every planet by the
same amount, so the chart stays internally consistent and simply
describes the wrong sky. This is the single highest-risk calculation in
the service.

It is also the piece `pyswisseph` would have given us for free. ADR-003
chose `skyfield` (MIT) and accepted, explicitly, that we would have to
*earn* agreement with the reference implementations rather than inherit
it. This module is where that debt is paid.

── How Lahiri is actually defined ──

Lahiri (Chitrapaksha) is often described as "the ayanamsa that puts
Chitra (Spica) at exactly 180° sidereal". That is the historical
motivation, not the operative definition, and implementing it literally
is wrong — measured, not assumed: computing Spica's longitude of date and
subtracting 180° lands about 35 arcseconds below the official value at
the very epoch Lahiri is defined at.

The operative definition comes from the Indian Calendar Reform Committee,
which fixed the ayanamsa at exactly 23°15'00" on 21 March 1956. Every
other value is that anchor carried forward or backward by the precession
of the equinoxes. That is what is implemented here.

── Why proper motion is excluded ──

Precession is a property of the Earth's axis. A real star also moves
across the sky on its own, and that proper motion is a contaminant in the
measurement, not part of it.

Stated precisely, because the obvious stronger claim is not supported by
what was measured: at the 1900 cross-validation epoch the two variants
differ by only about 1.5 arcseconds, and including proper motion is in
fact marginally CLOSER there. The 1900 test cannot distinguish them, and
does not pretend to.

The reason to exclude it is that the contamination grows without bound.
At J2000 the variants have already diverged by about 25 arcseconds, and
Spica's proper motion is roughly constant, so the gap keeps widening the
further a birth date sits from the anchor. Tracking a fixed direction has
no such term.

The decision is pinned by test_reference_direction_has_no_proper_motion
rather than by this comment, because a comment cannot stop someone
"improving" the reference by adding the catalogue values back.

Absolute accuracy: agreement with the independently published 1900 anchor
is about 5 arcseconds, or 0.0014°. A nakshatra is 13°20' wide and a pada
3°20'; the error is three orders of magnitude below the smallest boundary
any interpretation turns on.
"""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum
from typing import Final

from skyfield.api import Star
from skyfield.jpllib import SpiceKernel
from skyfield.timelib import Time, Timescale


class AyanamsaSystem(StrEnum):
    """Supported ayanamsas.

    Lahiri is the Indian standard and the default. The others are
    configuration, never hardcoded — a user who wants Raman must get
    Raman, and a chart computed under one must never be served for
    another, which is why the ayanamsa is part of both the chart's
    database identity and its cache key.
    """

    LAHIRI = "lahiri"
    RAMAN = "raman"
    KP = "kp"


#: The Indian Calendar Reform Committee's anchor: exactly 23°15'00" on
#: 21 March 1956. Not a fitted constant — a definition.
_LAHIRI_ANCHOR_VALUE: Final = 23.25
_LAHIRI_ANCHOR_DATE: Final = (1956, 3, 21)

#: Offsets from Lahiri at the anchor epoch, in degrees.
#:
#: Raman and KP share Lahiri's precession and differ by a constant, which
#: is how they are defined in practice. Stated as explicit offsets so the
#: relationship is visible rather than buried in three near-identical
#: implementations.
_OFFSET_FROM_LAHIRI: Final = {
    AyanamsaSystem.LAHIRI: 0.0,
    AyanamsaSystem.RAMAN: -1.1,  # Raman runs about 1.1° behind Lahiri
    AyanamsaSystem.KP: 0.0093,  # KP (Krishnamurti) runs fractionally ahead
}

#: Spica's J2000 position, used purely as a fixed direction on the
#: ecliptic from which to measure precession.
#:
#: Proper motion and parallax are deliberately omitted — see the module
#: docstring. The choice of Spica is historical rather than mathematical:
#: any fixed direction would measure the same precession.
_SPICA_RA_HOURS: Final = (13, 25, 11.579)
_SPICA_DEC_DEGREES: Final = (-11, 9, 40.75)


class AyanamsaCalculator:
    """Computes the ayanamsa for an instant.

    Holds the ephemeris and timescale rather than reaching for globals,
    so `core` stays pure: nothing here reads a clock, opens a socket or
    touches a file. The instant is always passed in.
    """

    def __init__(self, ephemeris: SpiceKernel, timescale: Timescale) -> None:
        self._earth = ephemeris["earth"]
        self._timescale = timescale

        # No proper motion, no parallax. This is a direction, not a star.
        self._reference = Star(
            ra_hours=_SPICA_RA_HOURS,
            dec_degrees=_SPICA_DEC_DEGREES,
        )

        anchor = self._timescale.utc(*_LAHIRI_ANCHOR_DATE)
        self._anchor_longitude = self._longitude_of_date(anchor)

    def _longitude_of_date(self, t: Time) -> float:
        """The reference direction's ecliptic longitude in the frame OF DATE.

        `epoch=t` is load-bearing and easy to omit. Without it skyfield
        returns coordinates in the J2000 ecliptic, the precession the
        whole calculation is trying to measure disappears, and the
        ayanamsa comes out very nearly constant across centuries — which
        is wrong in a way that still looks like a plausible number.
        """
        _, longitude, _ = (
            self._earth.at(t).observe(self._reference).apparent().ecliptic_latlon(epoch=t)
        )
        return float(longitude.degrees)

    def at(self, moment: datetime, system: AyanamsaSystem = AyanamsaSystem.LAHIRI) -> float:
        """The ayanamsa in degrees at a UTC instant."""
        if moment.tzinfo is None:
            raise ValueError(
                "ayanamsa needs an aware datetime; a naive one silently "
                "assumes a timezone and this service never guesses"
            )

        t = self._timescale.from_datetime(moment)
        return self.at_time(t, system)

    def at_time(self, t: Time, system: AyanamsaSystem = AyanamsaSystem.LAHIRI) -> float:
        """The ayanamsa for an already-constructed skyfield Time.

        Separate from `at` so the chart pipeline, which already holds a
        Time, does not round-trip through a datetime.
        """
        precession = self._longitude_of_date(t) - self._anchor_longitude
        return _LAHIRI_ANCHOR_VALUE + precession + _OFFSET_FROM_LAHIRI[system]

    def to_sidereal(
        self,
        tropical_longitude: float,
        t: Time,
        system: AyanamsaSystem = AyanamsaSystem.LAHIRI,
    ) -> float:
        """Convert a tropical longitude to sidereal, folded into [0, 360)."""
        return (tropical_longitude - self.at_time(t, system)) % 360.0
