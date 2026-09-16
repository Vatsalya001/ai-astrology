"""Rebuild tests/fixtures/charts/profiles.json.

Every profile here is INVENTED. None describes a real person — that is
what makes "no real birth data reaches a free model tier" enforceable
rather than aspirational.

The reason this is a script and not a hand-written JSON file is
`expected_utc_offset_min`. That field is an assertion about tzdata, and
the twelve hand-written profiles that preceded this got one of them
wrong: 009 claimed Calcutta was +05:53 in 1901, and tzdata says +05:21 —
Madras Mean Time, which British India ran on from 1870 until IST arrived
in 1906. A plausible number, asserted from reasoning rather than looked
up, and wrong.

So the offsets are derived from `zoneinfo` here, and asserted in Go
against `time/tzdata`. Two independent copies of the tz database have to
agree, which is a real check; one person's recollection is not.

Run:  uv run python scripts/build_profiles.py
"""

from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo

FIXTURES = Path(__file__).resolve().parents[3] / "tests" / "fixtures" / "charts" / "profiles.json"

# (id, note, date, time|None, accuracy, place, lat, lon, zone)
Case = tuple[str, str, str, str | None, str, str, float, float, str]

CASES: list[Case] = [
    # ─── The original twelve ─────────────────────────────────────
    (
        "001-delhi-1990-morning",
        "Baseline case. Nothing unusual.",
        "1990-03-14",
        "07:42",
        "exact",
        "New Delhi, Delhi, India",
        28.6139,
        77.2090,
        "Asia/Kolkata",
    ),
    (
        "002-mumbai-1985-midnight",
        "Twelve minutes past midnight. Julian day boundary.",
        "1985-11-02",
        "00:12",
        "exact",
        "Mumbai, Maharashtra, India",
        19.0760,
        72.8777,
        "Asia/Kolkata",
    ),
    (
        "003-kolkata-1943-wartime-dst",
        "India observed UTC+06:30 during WWII. Assuming +05:30 shifts the "
        "ascendant by roughly 15 degrees.",
        "1943-06-21",
        "16:20",
        "exact",
        "Kolkata, West Bengal, India",
        22.5726,
        88.3639,
        "Asia/Kolkata",
    ),
    (
        "004-london-1995",
        "Non-Indian timezone with British Summer Time in effect.",
        "1995-07-08",
        "13:05",
        "exact",
        "London, England, United Kingdom",
        51.5074,
        -0.1278,
        "Europe/London",
    ),
    (
        "005-anchorage-2000-high-latitude",
        "61 degrees north. Quadrant house systems degenerate near the poles; whole-sign does not.",
        "2000-12-05",
        "10:30",
        "exact",
        "Anchorage, Alaska, United States",
        61.2181,
        -149.9003,
        "America/Anchorage",
    ),
    (
        "006-quito-1988-equator",
        "Effectively on the equator. The opposite degenerate case to 005.",
        "1988-09-17",
        "18:55",
        "exact",
        "Quito, Pichincha, Ecuador",
        -0.1807,
        -78.4678,
        "America/Guayaquil",
    ),
    (
        "007-pune-1998-time-unknown",
        "Birth time genuinely unknown. Ascendant, houses and dashas MUST be null.",
        "1998-04-23",
        None,
        "unknown",
        "Pune, Maharashtra, India",
        18.5204,
        73.8567,
        "Asia/Kolkata",
    ),
    (
        "008-chennai-2000-leap-day",
        "Leap day in a century year that IS a leap year.",
        "2000-02-29",
        "21:47",
        "exact",
        "Chennai, Tamil Nadu, India",
        13.0827,
        80.2707,
        "Asia/Kolkata",
    ),
    (
        "009-kolkata-1901-local-mean-time",
        "Pre-IST India. tzdata puts Calcutta on Madras Mean Time (+05:21:10) "
        "from 1870 until IST in 1906. Calcutta Time (+05:53:20) remained in "
        "local use until 1948, so the history is genuinely ambiguous — this "
        "fixture asserts what tzdata says, because tzdata is what both "
        "services resolve through.",
        "1901-05-30",
        "09:15",
        "exact",
        "Kolkata, West Bengal, India",
        22.5726,
        88.3639,
        "Asia/Kolkata",
    ),
    (
        "010-jaipur-1994-ascendant-cusp",
        "Ascendant close to a sign boundary — the case where a small time "
        "error changes every house.",
        "1994-08-17",
        "14:35",
        "exact",
        "Jaipur, Rajasthan, India",
        26.9124,
        75.7873,
        "Asia/Kolkata",
    ),
    (
        "011-sydney-1979-southern-dst",
        "Southern hemisphere with summer time in January.",
        "1979-01-11",
        "05:20",
        "exact",
        "Sydney, New South Wales, Australia",
        -33.8688,
        151.2093,
        "Australia/Sydney",
    ),
    (
        "012-village-manual-coordinates",
        "Coordinates entered by hand rather than chosen from the gazetteer — "
        "the common real-world case for a village.",
        "1992-10-03",
        "11:08",
        "exact",
        "Karjat village, Maharashtra, India",
        18.9107,
        73.3230,
        "Asia/Kolkata",
    ),
    # ─── Offsets that are not whole hours ────────────────────────
    #
    # Any arithmetic that assumes a whole or half-hour offset survives the
    # twelve above and dies here.
    (
        "013-kathmandu-1987-quarter-hour-offset",
        "Nepal runs UTC+05:45. Code that stores an offset in hours, or "
        "assumes half-hour granularity, is wrong by 15 minutes — about "
        "3.75 degrees of ascendant.",
        "1987-06-12",
        "14:05",
        "exact",
        "Kathmandu, Bagmati, Nepal",
        27.7172,
        85.3240,
        "Asia/Kathmandu",
    ),
    (
        "014-chatham-2005-quarter-hour-dst",
        "The Chatham Islands run UTC+12:45, and +13:45 in summer. A "
        "quarter-hour offset AND daylight saving on top of it.",
        "2005-01-15",
        "09:30",
        "exact",
        "Waitangi, Chatham Islands, New Zealand",
        -43.9535,
        -176.5597,
        "Pacific/Chatham",
    ),
    (
        "015-tehran-2005-half-hour-dst",
        "Iran ran UTC+03:30 with summer time to +04:30, on a Persian-calendar "
        "schedule no northern-hemisphere assumption predicts.",
        "2005-07-10",
        "21:40",
        "exact",
        "Tehran, Tehran, Iran",
        35.6892,
        51.3890,
        "Asia/Tehran",
    ),
    (
        "016-adelaide-1995-half-hour-southern-dst",
        "South Australia is UTC+09:30, +10:30 in summer — a half-hour zone "
        "whose DST runs opposite to the northern hemisphere.",
        "1995-01-20",
        "03:15",
        "exact",
        "Adelaide, South Australia, Australia",
        -34.9285,
        138.6007,
        "Australia/Adelaide",
    ),
    (
        "017-lord-howe-2015-thirty-minute-dst-shift",
        "Lord Howe Island is the only place on Earth whose daylight saving "
        "shift is 30 minutes rather than an hour: +10:30 to +11:00.",
        "2015-01-15",
        "19:25",
        "exact",
        "Lord Howe Island, New South Wales, Australia",
        -31.5553,
        159.0821,
        "Australia/Lord_Howe",
    ),
    (
        "018-st-johns-2005-negative-half-hour",
        "Newfoundland is UTC-03:30, -02:30 in summer. The only NEGATIVE "
        "half-hour offset in use, and the one that catches integer division "
        "truncating towards zero instead of flooring.",
        "2005-07-04",
        "23:50",
        "exact",
        "St John's, Newfoundland, Canada",
        47.5615,
        -52.7126,
        "America/St_Johns",
    ),
    # ─── The international date line ─────────────────────────────
    (
        "019-apia-2010-east-of-the-date-line",
        "Samoa before the 2011 jump: UTC-11:00.",
        "2010-06-15",
        "08:00",
        "exact",
        "Apia, Tuamasaga, Samoa",
        -13.8507,
        -171.7514,
        "Pacific/Apia",
    ),
    (
        "020-apia-2012-west-of-the-date-line",
        "The SAME place after Samoa skipped 30 December 2011 entirely: "
        "UTC+13:00. A 24-hour swing at one location, which a cached zone "
        "offset would get catastrophically wrong.",
        "2012-06-15",
        "08:00",
        "exact",
        "Apia, Tuamasaga, Samoa",
        -13.8507,
        -171.7514,
        "Pacific/Apia",
    ),
    (
        "021-kiritimati-2000-furthest-forward",
        "UTC+14:00, the largest offset in use anywhere. A local date can be "
        "a full day ahead of UTC.",
        "2000-11-09",
        "06:00",
        "exact",
        "Kiritimati, Line Islands, Kiribati",
        1.8721,
        -157.4278,
        "Pacific/Kiritimati",
    ),
    # ─── The ephemeris kernel's edges ────────────────────────────
    (
        "022-kolkata-1900-kernel-lower-bound",
        "de421 begins in mid-1899. A birth in January 1900 is close enough "
        "to the edge that an off-by-one in the Julian day walks off the end "
        "of the kernel rather than returning a wrong answer.",
        "1900-01-15",
        "05:30",
        "exact",
        "Kolkata, West Bengal, India",
        22.5726,
        88.3639,
        "Asia/Kolkata",
    ),
    (
        "023-tokyo-2050-kernel-upper-bound",
        "de421 ends in 2053. Nobody is born in 2050 yet, but dasha "
        "projection reaches decades past a birth and this is where it stops.",
        "2050-06-01",
        "12:00",
        "exact",
        "Tokyo, Tokyo, Japan",
        35.6762,
        139.6503,
        "Asia/Tokyo",
    ),
    # ─── Astrological boundaries, found by search ────────────────
    #
    # These two were not invented. A search over the ephemeris found the
    # instants, so they sit within arcseconds of a boundary rather than
    # merely near one.
    (
        "024-delhi-2001-moon-on-a-nakshatra-boundary",
        "The Moon is at sidereal 359.9999 degrees — six thousandths of an "
        "arcminute below the Revati/Ashwini wrap, the last sliver of pada 4 "
        "before the zodiac restarts. Found by searching the ephemeris, not "
        "invented. It exercises the 359-to-0 wrap in the nakshatra, pada and "
        "dasha-lord lookups all at once. It is NOT a net for the "
        "float-division bug in subdivision_index: that only bites within one "
        "ulp of a boundary, and this is 3e-6 relative away. "
        "tests/test_subdivision.py is what covers that, by enumerating "
        "boundaries rather than hoping a planet lands on one.",
        "2001-01-04",
        "01:09",
        "exact",
        "New Delhi, Delhi, India",
        28.6139,
        77.2090,
        "Asia/Kolkata",
    ),
    (
        "025-delhi-1996-planet-on-a-sign-cusp",
        "Mercury is 0.6 arcminutes into sidereal Capricorn. Any rounding in "
        "the sign calculation puts it in Sagittarius and moves its house, "
        "its dignity and every aspect it casts.",
        "1996-02-09",
        "05:30",
        "exact",
        "New Delhi, Delhi, India",
        28.6139,
        77.2090,
        "Asia/Kolkata",
    ),
    # ─── Latitude extremes ───────────────────────────────────────
    (
        "026-tromso-1985-polar-night",
        "69.6 degrees north in December: the Sun does not rise. Whole-sign "
        "houses are still defined, which is the point — the ascendant "
        "formula must not divide by a cosine that has gone to zero.",
        "1985-12-21",
        "11:00",
        "exact",
        "Tromso, Troms, Norway",
        69.6492,
        18.9553,
        "Europe/Oslo",
    ),
    (
        "027-invercargill-1999-far-south",
        "46.4 degrees south, the mirror of 026 and the furthest-south "
        "inhabited case with its own DST rules.",
        "1999-11-20",
        "02:30",
        "exact",
        "Invercargill, Southland, New Zealand",
        -46.4132,
        168.3538,
        "Pacific/Auckland",
    ),
    # ─── Brackets around the wartime offset ──────────────────────
    #
    # 003 asserts +06:30 during the war. On their own, a bug that always
    # returned +06:30 for Kolkata would pass it. These two are the other
    # side of both boundaries.
    (
        "028-kolkata-1942-before-wartime-dst",
        "Three weeks BEFORE India moved to UTC+06:30. Brackets 003 from "
        "below: a bug that always returns the wartime offset passes 003 and "
        "fails here.",
        "1942-08-15",
        "10:00",
        "exact",
        "Kolkata, West Bengal, India",
        22.5726,
        88.3639,
        "Asia/Kolkata",
    ),
    (
        "029-kolkata-1945-after-wartime-dst",
        "After India returned to UTC+05:30. Brackets 003 from above.",
        "1945-12-01",
        "10:00",
        "exact",
        "Kolkata, West Bengal, India",
        22.5726,
        88.3639,
        "Asia/Kolkata",
    ),
    # ─── Unknown time, away from India ───────────────────────────
    (
        "030-reykjavik-1988-time-unknown-far-west",
        "Unknown birth time at a longitude far from the zone meridian. "
        "Iceland keeps UTC year-round despite sitting 21 degrees west, so "
        "local noon is over an hour from clock noon — which is exactly the "
        "assumption the unknown-time fallback makes.",
        "1988-05-14",
        None,
        "unknown",
        "Reykjavik, Capital Region, Iceland",
        64.1466,
        -21.9426,
        "Atlantic/Reykjavik",
    ),
]


def utc_offset_minutes(date: str, clock: str | None, zone: str) -> int:
    """The offset tzdata reports, truncated exactly as Go truncates it.

    Go's ResolveInstant does `offsetSeconds / 60` — integer division,
    which truncates TOWARDS ZERO. Python's `//` floors, and the two
    disagree for any negative offset that is not a whole minute. No zone
    in this list has one, but matching Go's arithmetic here means the
    fixture stays correct if one ever does.
    """
    moment = datetime.fromisoformat(f"{date}T{clock or '12:00'}").replace(tzinfo=ZoneInfo(zone))
    offset = moment.utcoffset()
    assert offset is not None
    return int(offset.total_seconds() / 60)


def build() -> dict[str, Any]:
    profiles: list[dict[str, Any]] = []

    for case in CASES:
        identifier, note, date, clock, accuracy, place, latitude, longitude, zone = case

        profile: dict[str, Any] = {
            "id": identifier,
            "note": note,
            "birth_date": date,
            "birth_time": clock,
            "time_accuracy": accuracy,
            "birth_place": place,
            "latitude": latitude,
            "longitude": longitude,
            "timezone": zone,
            "expected_utc_offset_min": utc_offset_minutes(date, clock, zone),
        }
        profiles.append(profile)

    identifiers = [p["id"] for p in profiles]
    duplicates = {i for i in identifiers if identifiers.count(i) > 1}
    if duplicates:
        raise SystemExit(f"duplicate fixture ids: {sorted(duplicates)}")

    return {
        "$comment": "SYNTHETIC DATA ONLY. Every profile here is invented. "
        "Generated by services/astro/scripts/build_profiles.py — edit that, not this.",
        "schema_version": 1,
        "profiles": profiles,
    }


def main() -> None:
    payload = build()
    FIXTURES.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(payload['profiles'])} profiles to {FIXTURES}")


if __name__ == "__main__":
    main()
