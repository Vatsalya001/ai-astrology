"""The seams Phase 5 fills, defined now so Phase 5 is additive.

PHASE-04 §9, step 7: "Step 7's context builders are Protocols with stub
implementations in this phase. Phase 5 fills them in. Defining the seams
now makes Phase 5 additive rather than a refactor."

── Why the stubs return empty rather than plausible text ──

A stub that returned "Sun in Leo, Moon in Scorpio" would make the whole
pipeline look like it worked, and the `fabricated_chart_fact` validator
would cheerfully check a model's claims against invented facts. Every
test would pass and the product would be confidently wrong in
development in exactly the way it must never be wrong in production.

Empty is honest: no chart was supplied, so the validator's empty-index
rule fires and a personal placement claim is blocked. That is the
correct behaviour for "we have no chart", and it means the stubbed
pipeline is safe rather than merely quiet.
"""

from __future__ import annotations

from typing import Protocol

from pydantic import BaseModel, Field

from app.classification import IntentResult
from app.validation import FactIndex


class ChartContext(BaseModel):
    """What `astro-service` computed, ready to put in a prompt.

    `text` is what the model reads; `facts` is what the validator checks
    against. They describe the same chart and MUST be built from the
    same source — a prose summary that drifted from the fact index would
    make the validator block correct answers, which is the failure mode
    most likely to look like a validator bug rather than a data bug.
    """

    text: str = ""
    facts: FactIndex = Field(default_factory=FactIndex)
    version: str = "none"
    """Which computation produced this, recorded on the response.

    `context_version` in `ai_request_logs`. Without it, a response from
    three weeks ago cannot be explained: the prompt version says what we
    asked and this says what we showed.
    """


class ConversationContext(BaseModel):
    summary: str = ""
    recent: str = ""
    version: str = "none"


class KnowledgeContext(BaseModel):
    """Phase 5's RAG results."""

    text: str = ""
    version: str = "none"


class ChartContextBuilder(Protocol):
    async def build(self, user_id: str, intent: IntentResult) -> ChartContext: ...


class ConversationContextBuilder(Protocol):
    async def build(self, conversation_id: str) -> ConversationContext: ...


class KnowledgeContextBuilder(Protocol):
    async def build(self, message: str, intent: IntentResult) -> KnowledgeContext: ...


class NoChartContext:
    """Phase 4's stub. Returns nothing, and says so in the version."""

    async def build(self, user_id: str, intent: IntentResult) -> ChartContext:
        return ChartContext()


class NoConversationContext:
    async def build(self, conversation_id: str) -> ConversationContext:
        return ConversationContext()


class NoKnowledgeContext:
    async def build(self, message: str, intent: IntentResult) -> KnowledgeContext:
        return KnowledgeContext()
