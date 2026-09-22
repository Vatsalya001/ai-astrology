"""The eleven steps, in the order that makes them safe and cheap.

PHASE-04 §9 lists the pipeline. This implements it with one deliberate
deviation, described below.

── The deviation: the free crisis check runs first, then two in parallel ──

The spec orders classification (step 4) before safety (step 5). Run
literally that is two sequential `fast`-tier calls on the critical path
of every message, and neither one's output feeds the other.

So:

  1. Keyword crisis detection — free, offline, instant. If it fires,
     nothing else runs at all and the static response returns with ZERO
     model calls.
  2. Intent and model-backed safety, CONCURRENTLY. Same semantics as the
     spec's ordering, roughly half the latency.

The cost of concurrency is one wasted classification when the model
screener flags a crisis that the keyword pass missed. That is rare, it
is one `fast`-tier call, and the alternative is adding a few hundred
milliseconds to every ordinary message in the product.

── What "bypasses astrology entirely" means here ──

`.claude/rules/ai.md`: "Crisis input bypasses astrology entirely and
returns a static, human-written response with a helpline. Never a
model-generated one. Never a prediction."

Structurally, not by convention: the crisis branch returns before any
context is built and before the generation provider is touched. The test
asserts the provider received nothing, which is the only assertion that
cannot pass while the bypass is broken.
"""

from __future__ import annotations

import asyncio
import logging
import time

from pydantic import BaseModel, Field

from app import pricing
from app.classification import Intent, IntentClassifier, IntentResult
from app.orchestrator.context import (
    ChartContextBuilder,
    ConversationContextBuilder,
    KnowledgeContextBuilder,
    NoChartContext,
    NoConversationContext,
    NoKnowledgeContext,
)
from app.orchestrator.envelope import AIResponseEnvelope, SafetyFlag, Telemetry
from app.prompts import PromptBuilder
from app.providers.base import (
    CallStats,
    CompletionRequest,
    CompletionResponse,
    LLMProvider,
    Message,
    ProviderError,
    RequestMetadata,
    Usage,
)
from app.routing import JobType, ModelRouter
from app.safety import (
    ABUSE_RESPONSE,
    SafetyCategory,
    SafetyClassifier,
    SafetyVerdict,
    declines,
    detect_crisis,
    detect_prompt_injection,
    load_crisis_response,
    posture_for,
    short_circuits,
)
from app.validation import OutputValidator, Violation

PERSONA_FOR: dict[Intent, str] = {
    Intent.CAREER: "career_guide",
    Intent.FINANCE: "career_guide",
    Intent.EDUCATION: "career_guide",
    Intent.RELATIONSHIP: "relationship_guide",
    Intent.MARRIAGE: "relationship_guide",
    Intent.COMPATIBILITY: "relationship_guide",
    Intent.FAMILY: "relationship_guide",
    Intent.EMOTIONAL_SUPPORT: "spiritual_guide",
}
DEFAULT_PERSONA = "vedic_guide"

log = logging.getLogger(__name__)


class CompleteRequest(BaseModel):
    """What `api-service` sends.

    `user_id` and `conversation_id` are IDs. No name, no email, no birth
    details — `.claude/rules/security.md`. The chart comes from
    `astro-service` via the context builder, keyed by the ID, and never
    travels through this request body.
    """

    message: str = Field(min_length=1, max_length=4000)
    user_id: str = ""
    conversation_id: str = ""
    job: JobType = JobType.CHAT_RESPONSE
    language: str = "en"


class CompleteResult(BaseModel):
    text: str
    intent: Intent = Intent.GENERAL_ASTROLOGY
    safety_category: SafetyCategory = SafetyCategory.NONE

    is_crisis_response: bool = False
    """Set when the static response was returned.

    The client renders this differently — no feedback buttons, no
    "regenerate", no share link. A crisis response is not a piece of
    product content and should not be offered as one.
    """

    blocked: bool = False
    """Validation failed twice and the graceful fallback is what the
    user sees. Distinct from a crisis: nothing was wrong with the
    QUESTION."""

    declined: bool = False
    """Abuse. The static refusal was returned and nothing was generated.

    Separate from `blocked` because the client renders them differently
    and because they mean opposite things: `blocked` is the product
    failing the user, `declined` is the product declining the message.
    """


# Human-written, like the crisis response and for a weaker version of
# the same reason: this is what a user sees when the model could not
# produce something the validator would pass, and asking a model to
# apologise for a model is how a second bad answer gets produced.
GRACEFUL_FALLBACK = (
    "I wasn't able to put that reading together properly just now. "
    "Could you ask me again, perhaps a little differently? "
    "If it keeps happening, that's on us rather than on your question."
)


class Orchestrator:
    """Steps 3 to 11. Steps 1 and 2 are middleware and startup guards."""

    def __init__(
        self,
        *,
        provider: LLMProvider,
        classifier: IntentClassifier,
        screener: SafetyClassifier,
        router: ModelRouter | None = None,
        chart_context: ChartContextBuilder | None = None,
        conversation_context: ConversationContextBuilder | None = None,
        knowledge_context: KnowledgeContextBuilder | None = None,
        prompt_version: str = "v1",
    ) -> None:
        self._provider = provider
        self._classifier = classifier
        self._screener = screener
        self._router = router or ModelRouter()
        self._chart = chart_context or NoChartContext()
        self._conversation = conversation_context or NoConversationContext()
        self._knowledge = knowledge_context or NoKnowledgeContext()
        self._prompt_version = prompt_version

    @property
    def router(self) -> ModelRouter:
        """The live router, so the admin PATCH mutates what serves.

        Deliberately the object and not a copy — `table` already hands
        out a copy for display. An admin override that mutated a copy
        would return 200 and change nothing, which is the worst possible
        outcome for a control an operator reaches for during an
        incident.
        """
        return self._router

    # ─── the crisis branch ───────────────────────────────────────────

    def _crisis_envelope(
        self,
        req: CompleteRequest,
        started: float,
        model_calls: int,
        *,
        trace_id: str,
        usage: Usage | None = None,
    ) -> AIResponseEnvelope[CompleteResult]:
        """The static response, with a telemetry row that says so.

        `cost_micros` is whatever the classification and screening cost
        and no more — this path GENERATES nothing, which is the whole
        point, and a telemetry row showing generation tokens here would
        mean the bypass leaked.

        It is not zero, though, and the first version recorded zero. A
        crisis reached via the model screener has already paid for a
        classification and a screening, both on a paid provider in
        production. Recording that as free would make the crisis path
        look costless in exactly the dashboard an operator would use to
        ask whether the safety layer is worth its price.
        """
        return AIResponseEnvelope(
            result=CompleteResult(
                text=load_crisis_response(req.language),
                safety_category=SafetyCategory.CRISIS,
                is_crisis_response=True,
            ),
            telemetry=Telemetry(
                trace_id=trace_id,
                user_id=req.user_id,
                conversation_id=req.conversation_id,
                job_type=req.job.value,
                # No provider, no model, no prompt version: nothing
                # generated this. Leaving them blank is the record that
                # the bypass worked.
                input_tokens=(usage or Usage()).input_tokens,
                output_tokens=(usage or Usage()).output_tokens,
                cached_tokens=(usage or Usage()).cached_input_tokens,
                cache_write_tokens=(usage or Usage()).cache_write_input_tokens,
                cost_micros=(usage or Usage()).cost_micros,
                latency_ms=int((time.monotonic() - started) * 1000),
                finish_reason="stop",
                safety_flags=[SafetyFlag(type="crisis", severity="block")],
                validation_passed=True,
                model_calls=model_calls,
            ),
        )

    def _declined_envelope(
        self,
        req: CompleteRequest,
        verdict: SafetyVerdict,
        started: float,
        model_calls: int,
        *,
        trace_id: str,
        usage: Usage | None = None,
    ) -> AIResponseEnvelope[CompleteResult]:
        """Abuse. A static refusal, on the same reasoning as crisis.

        There is nothing for a model to add, and asking one to compose a
        refusal is how a refusal turns into an argument — which is
        exactly what an abusive message is trying to start. §7's table
        says decline, log, and let rate limiting do the rest.
        """
        return AIResponseEnvelope(
            result=CompleteResult(
                text=ABUSE_RESPONSE,
                safety_category=verdict.category,
                declined=True,
            ),
            telemetry=Telemetry(
                trace_id=trace_id,
                user_id=req.user_id,
                conversation_id=req.conversation_id,
                job_type=req.job.value,
                # Blank, like the crisis path: nothing generated this.
                input_tokens=(usage or Usage()).input_tokens,
                output_tokens=(usage or Usage()).output_tokens,
                cached_tokens=(usage or Usage()).cached_input_tokens,
                cache_write_tokens=(usage or Usage()).cache_write_input_tokens,
                cost_micros=(usage or Usage()).cost_micros,
                latency_ms=int((time.monotonic() - started) * 1000),
                finish_reason="refusal",
                safety_flags=[SafetyFlag(type=verdict.category.value, severity="block")],
                validation_passed=True,
                model_calls=model_calls,
            ),
        )

    # ─── prompt assembly ─────────────────────────────────────────────

    def _persona(self, intent: Intent) -> str:
        return PERSONA_FOR.get(intent, DEFAULT_PERSONA)

    def _build_prompt(
        self,
        intent: IntentResult,
        chart_text: str,
        conversation_summary: str,
        conversation_recent: str,
        knowledge: str,
        correction: str = "",
        posture: str = "",
    ) -> PromptBuilder:
        builder = (
            PromptBuilder()
            .add("system_base", self._prompt_version)
            .add("astrology_rules", self._prompt_version)
            .add("safety_rules", self._prompt_version)
            .persona(self._persona(intent.primary), self._prompt_version)
            .add("output_format", self._prompt_version)
            # Everything above is identical for every user asking a
            # question of this persona. Everything below is theirs.
            # Everything above is identical for every user asking a
            # question of this persona. Everything below is theirs.
            .cache_breakpoint()
            .knowledge_context(knowledge)
            .chart_context(chart_text)
            .conversation_context(conversation_summary, conversation_recent)
        )

        if posture:
            # After the breakpoint, always. A posture placed among the
            # stable blocks would change the cached prefix whenever
            # anyone asked a health question — taking the hit rate to
            # zero for every OTHER user of the same persona, invisibly,
            # because every answer would still be correct.
            builder.instruction(posture)

        if correction:
            # Last, so it is the most recent thing the model read, and
            # after the breakpoint so it never pollutes the cached
            # prefix — a correction baked into the prefix would be sent
            # to every subsequent user.
            builder.instruction(correction)

        return builder

    # ─── generation ──────────────────────────────────────────────────

    async def _generate(self, req: CompleteRequest, builder: PromptBuilder) -> CompletionResponse:
        return await self._provider.complete(
            CompletionRequest(
                messages=[Message(role="user", content=req.message)],
                system=builder.build(),
                tier=self._router.tier_for(req.job),
                metadata=RequestMetadata(
                    user_id=req.user_id,
                    trace_id="",
                    prompt_version=self._prompt_version,
                    job=req.job.value,
                ),
            )
        )

    @staticmethod
    def _accumulate(total: Usage, one: Usage) -> Usage:
        """Sum token counts across every call in a request.

        Integers throughout, so the total is exact regardless of the
        order calls completed in — which matters because two of them run
        concurrently.
        """
        return Usage(
            input_tokens=total.input_tokens + one.input_tokens,
            output_tokens=total.output_tokens + one.output_tokens,
            cached_input_tokens=total.cached_input_tokens + one.cached_input_tokens,
            cache_write_input_tokens=(
                total.cache_write_input_tokens + one.cache_write_input_tokens
            ),
            cost_micros=total.cost_micros + one.cost_micros,
        )

    # ─── the pipeline ────────────────────────────────────────────────

    async def complete(
        self, req: CompleteRequest, *, trace_id: str = ""
    ) -> AIResponseEnvelope[CompleteResult]:
        started = time.monotonic()
        usage = Usage()
        calls = 0

        # ── 1. free, offline, first ──────────────────────────────────
        # Before anything that can fail or cost money, so a direct
        # expression of crisis is caught with every provider down. Zero
        # model calls on this path.
        if detect_crisis(req.message).category is SafetyCategory.CRISIS:
            return self._crisis_envelope(req, started, calls, trace_id=trace_id)

        # ── 2. intent and screening, concurrently ────────────────────
        # Neither feeds the other, so running them in sequence would add
        # a whole `fast`-tier round trip to the critical path of every
        # message for nothing. The screener re-runs the keyword pass
        # internally; that is a regex and costs nothing, and it keeps
        # SafetyClassifier usable on its own.
        intent, verdict = await asyncio.gather(
            self._classifier.classify(req.message, trace_id=trace_id),
            self._screener.screen(req.message, trace_id=trace_id),
        )

        # Counted from what each step REPORTS rather than inferred from
        # its `source`. The inference undercounted: a screening whose
        # reply was unparseable or below threshold reports
        # source="unparseable", which is not "model" — so the call that
        # was made and billed was not counted, and the requests that had
        # trouble looked like the cheap ones.
        for step in (intent.stats, verdict.stats):
            calls += step.calls
            usage = self._accumulate(usage, self._price_stats(step))

        # The model screener saw something the phrase list did not —
        # "I don't see the point of anything anymore" carries no crisis
        # keyword. Still a bypass: this returns before any context is
        # built and before the generation provider is touched.
        # Branch on the ACTION, not the category. §7 pairs every
        # category with one, and until now only SHORT_CIRCUIT was read:
        # a MEDICAL, LEGAL, ABUSE or PROMPT_INJECTION verdict was
        # computed, paid for and recorded — and changed nothing about
        # the answer, so §7's table was a statement of intent. Reading
        # ACTION_FOR means a category added later cannot be half-wired.
        # The offline injection pass, applied as an UPGRADE.
        #
        # Detection rested entirely on the model screener, so it did not
        # work during a provider outage and — an audit's finding — no
        # test ever handed an injection string to a real detector. Every
        # one fed the screener a stubbed verdict, which asserts the
        # category plumbing and nothing about detection.
        #
        # Only from NONE, never over a verdict the model already made: a
        # message can be both a crisis and an injection attempt, and
        # CRISIS must win. Upgrading downward would turn a safety
        # short-circuit into steering text.
        if verdict.category is SafetyCategory.NONE:
            offline = detect_prompt_injection(req.message)
            if offline.category is not SafetyCategory.NONE:
                # The screener's `stats` are kept: that call was made and
                # billed whatever it concluded, and dropping them would
                # hide the cost of a screening that missed something.
                verdict = offline.model_copy(update={"stats": verdict.stats})

        if short_circuits(verdict.category):
            return self._crisis_envelope(req, started, calls, trace_id=trace_id, usage=usage)

        if declines(verdict.category):
            return self._declined_envelope(
                req, verdict, started, calls, trace_id=trace_id, usage=usage
            )

        # ── 3. context (stubs in Phase 4) ────────────────────────────
        chart, conversation, knowledge = await asyncio.gather(
            self._chart.build(req.user_id, intent),
            self._conversation.build(req.conversation_id),
            self._knowledge.build(req.message, intent),
        )

        # ── 4. compose ───────────────────────────────────────────────
        posture = posture_for(verdict.category)

        builder = self._build_prompt(
            intent,
            chart.text,
            conversation.summary,
            conversation.recent,
            knowledge.text,
            posture=posture,
        )

        # `leakable`, not `cacheable_prefix`. The prefix stops at the
        # cache breakpoint, so the safety posture and the corrective
        # retry — both instructions — went unchecked, and §14 asks for
        # leak validation on ALL output. The chart and conversation
        # blocks are excluded by `leakable` on purpose: a user's own
        # chart is meant to be reflected back at them.
        validator = OutputValidator(system_prompt=builder.leakable)

        # ── 5. generate ──────────────────────────────────────────────
        try:
            response = await self._generate(req, builder)
        except ProviderError:
            return self._failed_envelope(req, intent, started, calls, usage, trace_id=trace_id)

        calls += 1
        usage = self._accumulate(usage, self._priced(response))

        # ── 6. validate, with exactly one corrective retry ───────────
        violations = validator.validate(response.text, chart.facts)
        regenerated = False

        if OutputValidator.blocks(violations):
            regenerated = True
            corrected = self._build_prompt(
                intent,
                chart.text,
                conversation.summary,
                conversation.recent,
                knowledge.text,
                correction=OutputValidator.corrective_instruction(violations),
                posture=posture,
            )
            try:
                response = await self._generate(req, corrected)
            except ProviderError:
                return self._failed_envelope(req, intent, started, calls, usage, trace_id=trace_id)

            calls += 1
            usage = self._accumulate(usage, self._priced(response))

            # A validator built from the CORRECTED prompt, not the first
            # one.
            #
            # The retry is sent with `corrected`, whose leakable text
            # carries the corrective instruction — and that instruction
            # quotes the violation: "You stated a placement the chart
            # does not support: saturn's house is 4, not 10."
            # Re-checking with the original `validator` scans for
            # shingles of a prompt the model was never shown, so when a
            # model does the obvious thing and parrots the instruction
            # back, the leak check finds nothing and the user is handed
            # our own internal correction text.
            #
            # Reproduced before fixing: the stale validator returns `[]`
            # on the exact correction string; one built from the
            # corrected prompt returns `prompt_leak` and blocks.
            #
            # §14 asks for leak validation on ALL output. The
            # regenerated answer is output.
            violations = OutputValidator(system_prompt=corrected.leakable).validate(
                response.text, chart.facts
            )

        blocked = OutputValidator.blocks(violations)

        return AIResponseEnvelope(
            result=CompleteResult(
                # The fallback, not the failing text. A response that
                # invented a placement must never reach a user, and
                # "show it with a warning" is not an option when the
                # warning is what the user would ignore.
                text=GRACEFUL_FALLBACK if blocked else response.text,
                intent=intent.primary,
                safety_category=verdict.category,
                blocked=blocked,
            ),
            telemetry=Telemetry(
                trace_id=trace_id,
                user_id=req.user_id,
                conversation_id=req.conversation_id,
                job_type=req.job.value,
                intent=intent.primary.value,
                provider_id=response.provider_id,
                model=response.model,
                tier=self._router.tier_for(req.job),
                prompt_version=self._prompt_version,
                context_version=chart.version,
                input_tokens=usage.input_tokens,
                output_tokens=usage.output_tokens,
                cached_tokens=usage.cached_input_tokens,
                cache_write_tokens=usage.cache_write_input_tokens,
                cost_micros=usage.cost_micros,
                latency_ms=int((time.monotonic() - started) * 1000),
                finish_reason=response.finish_reason,
                safety_flags=[SafetyFlag.of(v) for v in violations],
                validation_passed=not blocked,
                regenerated=regenerated,
                model_calls=calls,
            ),
        )

    # ─── pricing and failure ─────────────────────────────────────────

    @staticmethod
    def _price_stats(stats: CallStats) -> Usage:
        """A classification or screening call, costed.

        Same rate table as generation, keyed on the model that actually
        answered — which on a fallback chain is not the one configured.
        """
        if not stats.calls or not stats.model:
            return Usage()
        try:
            return pricing.priced(stats.model, stats.usage)
        except pricing.UnpricedModelError:
            log.warning(
                "unpriced model on a pipeline step; cost recorded as zero",
                extra={"model": stats.model},
            )
            return stats.usage

    @staticmethod
    def _priced(response: CompletionResponse) -> Usage:
        """Cost applied here, once, where the model name is known.

        An unpriced model raises inside `pricing`, which would fail the
        user's request over a bookkeeping gap. Caught and recorded as
        zero WITH the model name still in telemetry, so the gap is
        visible in the dashboard as a row costing nothing from a paid
        provider.

        Logged at WARNING as well, because "visible in the dashboard"
        assumed somebody was looking at the dashboard. A zero from an
        unpriced model and a zero from a local model are identical in
        that table; this line is the only thing that distinguishes them.
        """
        try:
            return pricing.priced(response.model, response.usage)
        except pricing.UnpricedModelError:
            log.warning(
                "unpriced model; cost recorded as zero. Add it to app/pricing.json.",
                extra={"model": response.model, "provider": response.provider_id},
            )
            return response.usage

    def _failed_envelope(
        self,
        req: CompleteRequest,
        intent: IntentResult,
        started: float,
        calls: int,
        usage: Usage,
        *,
        trace_id: str,
    ) -> AIResponseEnvelope[CompleteResult]:
        """Every provider in the chain failed.

        Still returns an envelope rather than raising: the tokens
        already spent on classification are real and must be recorded,
        and a request that cost money and produced no row is a hole in
        the bill.
        """
        return AIResponseEnvelope(
            result=CompleteResult(text=GRACEFUL_FALLBACK, intent=intent.primary, blocked=True),
            telemetry=Telemetry(
                # Carried, not blanked. trace_id is NOT NULL in
                # ai_request_logs and is the only join back to the Go
                # request and the log lines of both services — so a
                # failure row written without one is the row an operator
                # most wants to find and cannot.
                trace_id=trace_id,
                user_id=req.user_id,
                conversation_id=req.conversation_id,
                job_type=req.job.value,
                intent=intent.primary.value,
                tier=self._router.tier_for(req.job),
                input_tokens=usage.input_tokens,
                output_tokens=usage.output_tokens,
                # Both cache classes were dropped here while a non-zero
                # cost was still recorded, so a failed request's tokens
                # could not be reconciled against its own charge.
                cached_tokens=usage.cached_input_tokens,
                cache_write_tokens=usage.cache_write_input_tokens,
                cost_micros=usage.cost_micros,
                latency_ms=int((time.monotonic() - started) * 1000),
                finish_reason="error",
                validation_passed=False,
                model_calls=calls,
            ),
        )


__all__ = [
    "DEFAULT_PERSONA",
    "GRACEFUL_FALLBACK",
    "PERSONA_FOR",
    "CompleteRequest",
    "CompleteResult",
    "Orchestrator",
    "Violation",
]
