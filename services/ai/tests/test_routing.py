"""Job → tier. Ten jobs, and what happens to an eleventh."""

from __future__ import annotations

import pytest

from app.routing import DEFAULT_ROUTING, JobType, ModelRouter, UnroutableJobError


def test_every_job_type_has_a_tier() -> None:
    """The gate item: "Model router maps all 10 job types".

    Derived from the enum rather than from a hardcoded count, so adding
    an eleventh job fails HERE — at the place where the cost decision
    belongs — instead of at the first request that uses it.
    """
    missing = [job for job in JobType if job not in DEFAULT_ROUTING]

    assert missing == [], (
        f"these jobs have no tier: {missing}. An unrouted job silently taking the "
        f"cheapest tier produces bad answers; silently taking the most expensive "
        f"produces a bill."
    )


def test_classification_work_is_cheap() -> None:
    # The whole economic argument of the table. If these drift to `deep`,
    # a 21-way label starts costing deep-reasoning prices on every turn.
    router = ModelRouter()
    for job in (
        JobType.INTENT_CLASSIFICATION,
        JobType.SAFETY_CLASSIFICATION,
        JobType.MEMORY_EXTRACTION,
    ):
        assert router.tier_for(job) == "fast", f"{job} is no longer on the cheap tier"


def test_paid_output_is_on_the_deep_tier() -> None:
    router = ModelRouter()
    for job in (JobType.CHART_INTERPRETATION, JobType.PREMIUM_REPORT, JobType.COMPATIBILITY):
        assert router.tier_for(job) == "deep"


def test_an_override_changes_one_job() -> None:
    router = ModelRouter({JobType.CHAT_RESPONSE: "deep"})

    assert router.tier_for(JobType.CHAT_RESPONSE) == "deep"


def test_an_override_does_not_unroute_the_others() -> None:
    """Overrides are applied OVER the defaults, not instead of them.

    A whole-table replacement is the mistake an operator makes at 3am:
    change one job, and the other nine vanish. Merging means the worst
    case of a bad override is one wrong tier.
    """
    router = ModelRouter({JobType.CHAT_RESPONSE: "deep"})

    assert len(router.table) == len(JobType)
    assert router.tier_for(JobType.INTENT_CLASSIFICATION) == "fast"


def test_the_table_handed_out_is_a_copy() -> None:
    # Otherwise an admin endpoint that renders routing can mutate it for
    # the whole process by accident.
    router = ModelRouter()
    router.table[JobType.CHAT_RESPONSE] = "deep"

    assert router.tier_for(JobType.CHAT_RESPONSE) == "chat"


def test_an_unknown_job_raises_its_own_error_type() -> None:
    """Not a bare KeyError.

    Three frames up, a KeyError is indistinguishable from any other
    missing key, and the reflex fix is a defensive `.get()` with a
    default — which is precisely the silent mis-routing this type exists
    to prevent.
    """
    router = ModelRouter()

    with pytest.raises(UnroutableJobError):
        router.tier_for("not_a_job")  # type: ignore[arg-type]
