"""Job → tier → model, and the ten jobs this product runs.

── Why two hops rather than one ──

A call site knows what it is DOING — classifying an intent, writing a
paid interpretation — and should never know which model serves that. Job
→ tier is a judgement about cost and quality that belongs in one table;
tier → model is a per-provider fact that belongs in the adapter. Collapse
them and every call site hardcodes a model name, which is how a provider
swap becomes a hundred-file change.

── The table is a starting configuration, not a conclusion ──

The spec is blunt about this: "the cheapest model that holds quality on a
given route is an empirical question, and the answer differs per route."
Phase 6's eval harness turns it into a measurement. Until then, changing
a mapping should be cheap and reversible, which is why overrides exist
and why they are data rather than code.
"""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, Field

from app.providers.base import ModelTier


class JobType(StrEnum):
    """Every distinct thing this product asks a model to do.

    `StrEnum` so a job survives a round trip through JSON, an admin
    override and a log line as itself rather than as an integer nobody
    can read in a dashboard.
    """

    INTENT_CLASSIFICATION = "intent_classification"
    SAFETY_CLASSIFICATION = "safety_classification"
    MEMORY_EXTRACTION = "memory_extraction"
    CONVERSATION_SUMMARY = "conversation_summary"
    SUGGESTED_QUESTIONS = "suggested_questions"
    DAILY_HOROSCOPE = "daily_horoscope"
    CHAT_RESPONSE = "chat_response"
    CHART_INTERPRETATION = "chart_interpretation"
    PREMIUM_REPORT = "premium_report"
    COMPATIBILITY = "compatibility"


DEFAULT_ROUTING: dict[JobType, ModelTier] = {
    # Mechanical work: a label, a field, a summary. A 3B model does these
    # as well as a frontier one and costs a twentieth as much.
    JobType.INTENT_CLASSIFICATION: "fast",
    JobType.SAFETY_CLASSIFICATION: "fast",
    JobType.MEMORY_EXTRACTION: "fast",
    JobType.CONVERSATION_SUMMARY: "fast",
    JobType.SUGGESTED_QUESTIONS: "fast",
    # Conversation. Quality is visible to the user in every sentence.
    JobType.DAILY_HOROSCOPE: "chat",
    JobType.CHAT_RESPONSE: "chat",
    # Paid, long-form, and read once carefully rather than skimmed. The
    # only place deep-reasoning prices are defensible.
    JobType.CHART_INTERPRETATION: "deep",
    JobType.PREMIUM_REPORT: "deep",
    JobType.COMPATIBILITY: "deep",
}


class UnroutableJobError(KeyError):
    """A job with no tier.

    Its own type because the alternative — a `KeyError` from a dict — is
    indistinguishable from any other missing key three frames up, and the
    fix is completely different: a new job needs a deliberate cost
    decision, not a defensive `.get()`.
    """


class ModelRouter:
    """Which tier serves a job, with overrides that need no deploy.

    Overrides are applied over the defaults rather than replacing them,
    so an operator changing one job cannot accidentally unroute the other
    nine — which is exactly the mistake a whole-table replacement invites
    at 3am.
    """

    def __init__(self, overrides: dict[JobType, ModelTier] | None = None) -> None:
        self._routing: dict[JobType, ModelTier] = {**DEFAULT_ROUTING, **(overrides or {})}

    def tier_for(self, job: JobType) -> ModelTier:
        try:
            return self._routing[job]
        except KeyError:
            raise UnroutableJobError(
                f"job {job!r} has no tier. Add it to DEFAULT_ROUTING with a deliberate "
                f"cost decision rather than defaulting — an unrouted job silently "
                f"taking the cheapest tier produces bad answers, and one silently "
                f"taking the most expensive produces a bill."
            ) from None

    def apply(self, overrides: list[RoutingOverride]) -> dict[JobType, ModelTier]:
        """Change routing at runtime. §17: "overridable from admin
        without deploy".

        The gate item said "without a deploy" and the class only took
        overrides in its CONSTRUCTOR — which means a deploy. The admin
        endpoint listed in §10 as `PATCH /admin/ai/config` did not exist
        either, so the whole mechanism was a constructor argument that
        only tests passed.

        Applied OVER the defaults, one job at a time, never replacing
        the table: an operator changing one mapping at 3am cannot
        accidentally unroute the other nine. The absence of a
        "replace the whole table" method is the design.

        Returns the resulting table so the caller can echo exactly what
        took effect rather than what it asked for.
        """
        for override in overrides:
            self._routing[override.job] = override.tier
        return self.table

    def reset(self) -> dict[JobType, ModelTier]:
        """Back to DEFAULT_ROUTING.

        The other half of a runtime override. Without it the only way
        back from a bad 3am change is the deploy the override existed to
        avoid.
        """
        self._routing = dict(DEFAULT_ROUTING)
        return self.table

    @property
    def overridden(self) -> dict[JobType, ModelTier]:
        """Only the jobs that differ from the default.

        What an operator actually wants to see: "what did somebody
        change", not "what are all ten mappings". A mapping that differs
        without a stated reason is indistinguishable from a mistake six
        months later — which is why `RoutingOverride` carries one.
        """
        return {
            job: tier for job, tier in self._routing.items() if DEFAULT_ROUTING.get(job) != tier
        }

    @property
    def table(self) -> dict[JobType, ModelTier]:
        """A copy, for the admin endpoint to display.

        A copy rather than the live dict: handing out the internal
        mapping means an endpoint that renders it can mutate routing for
        the whole process by accident.
        """
        return dict(self._routing)


class RoutingOverride(BaseModel):
    """One admin change, validated before it reaches the router.

    A Pydantic model rather than a raw dict so an override arriving from
    the Go admin panel with a typo'd job name fails at the boundary with
    a readable message, rather than silently adding a key nothing reads.
    """

    job: JobType
    tier: ModelTier
    reason: str = Field(
        default="",
        description="Why. Shown in the admin UI next to the override, because a "
        "mapping that differs from the default without a stated reason is "
        "indistinguishable from a mistake six months later.",
    )
