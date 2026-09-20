"""What Python returns and Go persists.

PHASE-04 §8: `ai-service` holds a read-only role, so it cannot write its
own logs. "That constraint turns out to be a feature: Python returns
telemetry in the response envelope and Go persists it, keeping one
writer and one transaction boundary."

── The field that is NOT here ──

Message content. Not the user's question, not the model's answer, not a
violation excerpt. `.claude/rules/security.md` and PHASE-04 §14:
"`ai_request_logs` stores IDs and token counts — never message content."

`safety_flags` therefore carries violation TYPES and severities only.
That is a real cost — a reviewer looking at a blocked response in the
admin panel sees `fabricated_chart_fact / block` rather than the
sentence — and it is the right trade: this table is retained, replicated
and read by a billing job in Phase 7, and an excerpt of a user's reading
about their marriage does not belong in any of those places.
"""

from __future__ import annotations

from pydantic import BaseModel, Field

from app.providers.base import FinishReason
from app.validation import Violation


class SafetyFlag(BaseModel):
    """One finding, without the text that produced it."""

    type: str
    severity: str

    @classmethod
    def of(cls, violation: Violation) -> SafetyFlag:
        return cls(type=violation.type, severity=violation.severity)


class Telemetry(BaseModel):
    """One row of `ai_request_logs`, assembled by Python.

    Field names match the migration column-for-column on purpose. The Go
    side maps this straight through, and a rename on either side that
    does not happen on both is caught by the generated client rather
    than by a column silently receiving zero.
    """

    trace_id: str
    user_id: str = ""
    conversation_id: str = ""

    job_type: str
    intent: str = ""

    provider_id: str = ""
    model: str = ""
    tier: str = ""

    prompt_version: str = ""
    context_version: str = ""

    input_tokens: int = Field(default=0, ge=0)
    output_tokens: int = Field(default=0, ge=0)
    cached_tokens: int = Field(default=0, ge=0)
    cache_write_tokens: int = Field(default=0, ge=0)

    latency_ms: int = Field(default=0, ge=0)

    cost_micros: int = Field(default=0, ge=0, json_schema_extra={"format": "int64"})
    """Integer micro-USD. `.claude/CLAUDE.md` invariant 4.

    `BIGINT` in Postgres, `int` here, and never a float in between. This
    column is what Phase 7's usage billing reads.

    `format: int64` is declared so the generated Go client types this as
    `*int64` rather than `*int`. On this machine they are the same width;
    on a 32-bit build they are not, and `.claude/rules/go.md` is explicit
    that money is `int64` rather than whatever `int` happens to mean.
    Declaring it costs one keyword and removes the platform from the
    question.
    """

    finish_reason: FinishReason = "stop"

    safety_flags: list[SafetyFlag] = Field(default_factory=list)
    validation_passed: bool = True

    regenerated: bool = False
    """Whether the single corrective retry was spent.

    Not in the spec's table, and added because it is the number that
    says whether output validation is costing real money: a regenerated
    response is two full generations for one answer.
    """

    model_calls: int = Field(default=0, ge=0)
    """Every model call this request made, including classification.

    PHASE-04 §8 wants cost per completed TASK, not per request — "a
    cheap request needing three retries isn't cheap". This is the
    denominator that makes that measurable.
    """


class AIResponseEnvelope[T](BaseModel):
    """The result, and everything Go needs to record about producing it."""

    result: T
    telemetry: Telemetry
