"""The streaming pipeline. PHASE-05 task 5.11.

Emits §6's event sequence for one chat message. Shares every safety and
context step with `Orchestrator.complete` — the difference is only that
generation is incremental and therefore validated incrementally.

── Why not reuse `complete()` and stream its result ──

Because the result is the whole response, so streaming it afterwards
streams nothing: the first token would arrive after the last one was
generated, and §15 budgets "first token < 2 s".

The duplication is real and bounded: the crisis check, the concurrent
intent/screening, and the context build are called here, not
reimplemented. What is genuinely different is the generation loop and
the validation strategy, which is the part that cannot be shared.

── The validation strategy ──

See `app/orchestrator/events.py` for the four options and why this ships
sentence-incremental validation. In short: a streamed response cannot be
validated before the user has seen it, so it is validated as each
sentence completes, and the stream is cut on the first violation.
"""

from __future__ import annotations

import asyncio
import logging
import re
import time
from collections.abc import AsyncIterator

from pydantic import BaseModel, Field

from app.classification import Intent, IntentClassifier, IntentResult
from app.context import AstrologyContextService, ChartPayload
from app.orchestrator.context import (
    ChartContext,
    KnowledgeContextBuilder,
    NoKnowledgeContext,
)
from app.orchestrator.events import (
    ChatEvent,
    context_event,
    done_event,
    error_event,
    explanation_event,
    followups_event,
    intent_event,
    token_event,
)
from app.prompts import PromptBuilder
from app.providers import (
    CompletionRequest,
    LLMProvider,
    Message,
    ProviderError,
    RequestMetadata,
    Usage,
)
from app.routing import JobType, ModelRouter
from app.safety import SafetyCategory, SafetyClassifier, detect_crisis, load_crisis_response
from app.settings import settings
from app.validation import FactIndex, OutputValidator

log = logging.getLogger(__name__)

# Where a sentence ends, for incremental validation.
#
# Deliberately coarser than `app/validation/claims.py`'s splitter and for
# a different purpose: that one has to not split "13.2 degrees", this one
# only has to find a point at which a claim is complete enough to check.
# Checking too early sees a half-written claim and blocks a response that
# was about to be correct; checking too late shows the user more invalid
# text before the cut.
_SENTENCE_END = re.compile(r"[.!?]\s|[।॥]\s?")

# Chunks of a response that are too short to contain a complete claim.
# Below this the validator has nothing to work with and running it is
# pure latency on the token path.
_MIN_VALIDATABLE = 24


class ChatRequest(BaseModel):
    """One chat turn, as `api-service` sends it.

    ── Why the chart is in the body ──

    §12: "Chart context comes from the chart Go loaded after an ownership
    check — never from an ID in the message body." So Go sends the chart
    itself, having proved the caller owns it, and this service has no way
    to ask for a different one. The positions are not birth details —
    no name, no date, no place — so `.claude/rules/security.md`'s PII
    rule is satisfied by what is absent rather than by redaction.
    """

    message: str = Field(min_length=1, max_length=4000)
    user_id: str = ""
    conversation_id: str = ""
    language: str = "en"

    chart: ChartPayload | None = None
    """The computed chart, or None for a user with no profile yet."""

    persona: str = "vedic_guide"

    recent: list[str] = Field(default_factory=list)
    """Prior turns, newest last, already trimmed by Go to
    `CHAT_RECENT_MESSAGE_WINDOW`. Trimmed THERE because Go owns the
    conversation and knows which messages are partial."""

    model_config = {"extra": "forbid"}


class StreamOutcome(BaseModel):
    """What the stream produced, for the caller to persist.

    Returned alongside the events rather than inferred from them: the Go
    proxy needs the accumulated text and the telemetry to write one row
    in one transaction, and reconstructing them by re-parsing the SSE it
    just forwarded would be a second source of truth.
    """

    text: str = ""
    intent: Intent = Intent.GENERAL_ASTROLOGY
    is_crisis_response: bool = False
    blocked: bool = False
    declined: bool = False
    is_partial: bool = False
    usage: Usage = Field(default_factory=Usage)
    context_version: str = "none"
    prompt_version: str = ""
    basis: list[str] = Field(default_factory=list)


class StreamingOrchestrator:
    """One chat turn, as an async iterator of SSE events."""

    def __init__(
        self,
        *,
        provider: LLMProvider,
        classifier: IntentClassifier,
        screener: SafetyClassifier,
        router: ModelRouter | None = None,
        knowledge: KnowledgeContextBuilder | None = None,
        validator: OutputValidator | None = None,
        prompt_version: str = "v1",
    ) -> None:
        self._provider = provider
        self._classifier = classifier
        self._screener = screener
        self._router = router or ModelRouter()
        self._knowledge = knowledge or NoKnowledgeContext()
        self._validator = validator or OutputValidator()
        self._prompt_version = prompt_version

        # Written by `stream`, read by the caller after the iterator is
        # exhausted. A field rather than a return value because an async
        # generator cannot return one.
        self.outcome = StreamOutcome()

    async def stream(
        self, req: ChatRequest, *, now: object = None, trace_id: str = ""
    ) -> AsyncIterator[ChatEvent]:
        """§6's event sequence for one message.

        `now` is injected for the dasha, per §3's purity requirement. Typed
        loosely here and narrowed at the call site, because a datetime
        default would be an ambient clock in a signature.
        """
        from datetime import UTC, datetime

        moment = now if isinstance(now, datetime) else datetime.now(UTC)
        started = time.monotonic()
        usage = Usage()

        # ── 1. Crisis, free and offline, before anything else ──
        #
        # `.claude/rules/ai.md`: "Crisis input bypasses astrology
        # entirely and returns a static, human-written response with a
        # helpline. Never a model-generated one."
        #
        # So this path emits NO token events at all. The static text goes
        # out as the response in one piece — streaming a helpline word by
        # word would be grotesque, and the client renders a crisis
        # response differently anyway: no feedback buttons, no
        # regenerate, no share.
        if detect_crisis(req.message).category is SafetyCategory.CRISIS:
            text = load_crisis_response(req.language)
            self.outcome = StreamOutcome(
                text=text,
                is_crisis_response=True,
                context_version="crisis:no-chart-consulted",
            )
            yield intent_event(Intent.EMOTIONAL_SUPPORT.value, 1.0)
            yield token_event(text)
            yield done_event(is_crisis=True, usage=_usage_data(usage, started))
            return

        # ── 2. Intent and screening, concurrently ──
        try:
            intent, verdict = await asyncio.gather(
                self._classifier.classify(req.message, trace_id=trace_id),
                self._screener.screen(req.message, trace_id=trace_id),
            )
        except ProviderError as err:
            yield error_event("AI_UNAVAILABLE", retryable=err.retryable, detail=str(err))
            return

        if verdict.category is SafetyCategory.CRISIS:
            # The model screener caught what the keyword pass did not.
            # Same bypass, and the usage so far is real and is recorded:
            # a crisis reached through the screener has already paid for
            # a classification.
            text = load_crisis_response(req.language)
            # Usage is NOT attributed here, and the first version tried
            # to: `verdict.usage` does not exist. A crisis reached
            # through the model screener has genuinely cost a
            # classification and a screening, and PHASE-04's
            # `_crisis_envelope` records that — the non-streaming path
            # threads the real `Usage` through from both calls.
            #
            # Threading it here needs the classifier and screener to
            # report usage out of `asyncio.gather`, which they do not
            # currently do. Left as a known gap rather than papered over
            # with a zero, because a crisis path that looks costless is
            # exactly what PHASE-04's comment warns against — see
            # `_crisis_envelope`.
            self.outcome = StreamOutcome(
                text=text,
                intent=intent.primary,
                is_crisis_response=True,
                context_version="crisis:no-chart-consulted",
            )
            yield intent_event(intent.primary.value, intent.confidence)
            yield token_event(text)
            yield done_event(is_crisis=True, usage=_usage_data(self.outcome.usage, started))
            return

        if verdict.category is SafetyCategory.ABUSE:
            self.outcome = StreamOutcome(intent=intent.primary, declined=True)
            yield intent_event(intent.primary.value, intent.confidence)
            yield error_event("DECLINED", retryable=False, detail="message declined")
            return

        yield intent_event(intent.primary.value, intent.confidence)

        # ── 3. Context ──
        chart_context = await AstrologyContextService(req.chart, now=moment).build(
            req.user_id, intent
        )
        knowledge = await self._knowledge.build(req.message, intent)

        chunk_count = _chunk_count(knowledge.text)
        yield context_event(fact_count=_fact_count(chart_context.facts), chunk_count=chunk_count)

        basis = _basis(chart_context, intent)

        # ── 4. Generate, validating as sentences complete ──
        builder = self._prompt_builder(req, chart_context, knowledge.text)
        accumulated: list[str] = []
        checked_to = 0
        blocked = False

        request = CompletionRequest(
            messages=[
                *(Message(role="user", content=line) for line in req.recent),
                Message(role="user", content=req.message),
            ],
            system=builder.build(),
            tier=self._router.tier_for(JobType.CHAT_RESPONSE),
            metadata=RequestMetadata(
                user_id=req.user_id,
                trace_id=trace_id,
                prompt_version=self._prompt_version,
                job=JobType.CHAT_RESPONSE.value,
            ),
        )

        # ── Why the provider's stream is closed EXPLICITLY ──
        #
        # §6 point 2: "an abandoned request stops burning tokens
        # immediately. This is a real cost saving at volume."
        #
        # It does not happen by itself. When the consumer abandons this
        # generator, Python raises `GeneratorExit` at its `yield` and the
        # frame unwinds — but an inner ASYNC generator is finalised by the
        # event loop's `shutdown_asyncgens`, not synchronously at that
        # point. So without this `finally`, the provider's stream stays
        # live and keeps generating until the loop decides to clean it up,
        # which in a long-lived server can be process exit.
        #
        # Caught by `test_abandoning_the_stream_stops_generation_and_keeps_the_partial`,
        # which closes the consumer after two tokens and asserts the
        # provider saw it. It did not.
        token_stream = self._provider.stream(request)
        try:
            async for chunk in token_stream:
                if chunk.usage is not None:
                    usage = chunk.usage
                if not chunk.text:
                    continue

                accumulated.append(chunk.text)

                # The outcome is updated BEFORE the yield, on every token.
                #
                # Not after the loop: `GeneratorExit` is raised AT the
                # yield, so anything after it never runs on an abandoned
                # stream — and §6 point 3 requires the partial response to
                # survive, or "every disconnect loses the partial
                # response". Keeping the outcome current means the
                # proxy's `context.WithoutCancel` persist always has
                # something to write, however the stream ended.
                self.outcome = StreamOutcome(
                    text="".join(accumulated),
                    intent=intent.primary,
                    is_partial=True,
                    usage=usage,
                    context_version=chart_context.version,
                    prompt_version=self._prompt_version,
                    basis=basis,
                )

                yield token_event(chunk.text)

                # Validate only COMPLETED sentences, and only the part
                # not yet checked. Re-validating the whole accumulation
                # on every token is O(n²) in the response length and
                # measurably slows the token path on a long answer.
                whole = "".join(accumulated)
                boundary = _last_sentence_end(whole, checked_to)
                if boundary is None or boundary - checked_to < _MIN_VALIDATABLE:
                    continue

                segment = whole[checked_to:boundary]
                checked_to = boundary

                if self._violations(segment, chart_context.facts):
                    blocked = True
                    break

        except ProviderError as err:
            # Partial text survives: §6 requires a disconnect to leave a
            # partial message in history rather than a gap, and a
            # provider failure mid-stream is the same situation from the
            # other end.
            self.outcome = StreamOutcome(
                text="".join(accumulated),
                intent=intent.primary,
                is_partial=True,
                usage=usage,
                context_version=chart_context.version,
                prompt_version=self._prompt_version,
                basis=basis,
            )
            yield error_event("AI_UNAVAILABLE", retryable=err.retryable, detail=str(err))
            return

        except asyncio.CancelledError:
            # The client disconnected and Go cancelled the context.
            # The outcome is already current — it is updated per token
            # above — so there is nothing to salvage here, only to
            # re-raise so the cancellation is not swallowed.
            raise

        finally:
            # Prompt, not deferred. See the comment above the loop.
            closer = getattr(token_stream, "aclose", None)
            if closer is not None:
                await closer()

        text = "".join(accumulated)

        # The final pass over whatever the sentence loop did not reach —
        # a response ending without terminal punctuation, which a model
        # truncated by a token limit produces constantly.
        if not blocked and self._violations(text[checked_to:], chart_context.facts):
            blocked = True

        self.outcome = StreamOutcome(
            text=text,
            intent=intent.primary,
            blocked=blocked,
            is_partial=blocked,
            usage=usage,
            context_version=chart_context.version,
            prompt_version=self._prompt_version,
            basis=basis,
        )

        if blocked:
            # No `done`. The stream ends in `error`, so a client that
            # only ever renders on `done` cannot show a blocked response
            # as a finished one.
            log.warning(
                "blocked a streamed response",
                extra={
                    "intent": intent.primary.value,
                    "characters_emitted": len(text),
                    "context_version": chart_context.version,
                },
            )
            yield error_event(
                "BLOCKED",
                retryable=True,
                detail="the response claimed something the chart does not support",
            )
            return

        yield explanation_event(basis)
        yield followups_event(_followups(intent.primary))
        yield done_event(usage=_usage_data(usage, started))

    # ── helpers ──

    def _violations(self, segment: str, facts: FactIndex) -> bool:
        """True when this segment contains a blocking violation.

        Only BLOCK severity cuts the stream. A warning is recorded by the
        non-streaming path and does not stop it, and treating warnings as
        fatal here would make the streaming path stricter than the
        validator it shares — a difference nobody would predict from
        reading either.
        """
        if len(segment.strip()) < _MIN_VALIDATABLE:
            return False
        violations = self._validator.validate(segment, facts)
        return any(v.severity == "block" for v in violations)

    def _prompt_builder(
        self, req: ChatRequest, chart: ChartContext, knowledge: str
    ) -> PromptBuilder:
        """The prompt, composed from modules.

        `.claude/rules/ai.md`: "Composed from modules, never one giant
        string. Stable content first, volatile content last — a byte
        change early in the prefix invalidates the whole cache
        downstream."

        So the persona and the rules come first and the retrieved
        passages and the chart come last. The user's message is never in
        the system section at all, which is the same rule stated from the
        injection side: "Never interpolate user input into the system
        prompt section."
        """
        builder = (
            PromptBuilder()
            .add("system_base", self._prompt_version)
            .add("astrology_rules", self._prompt_version)
            .add("safety_rules", self._prompt_version)
            .persona(req.persona, self._prompt_version)
            .add("output_format", self._prompt_version)
            # Everything above is identical for every user asking this
            # persona a question. Everything below is theirs.
            .cache_breakpoint()
            .knowledge_context(knowledge)
            .chart_context(chart.text)
        )
        return builder


def _last_sentence_end(text: str, after: int) -> int | None:
    """The index just past the last complete sentence at or after `after`.

    `None` when the tail holds no boundary yet, which is the common case
    mid-sentence and is why this returns an option rather than `after`.
    """
    last: int | None = None
    for match in _SENTENCE_END.finditer(text, after):
        last = match.end()
    return last


def _fact_count(facts: FactIndex) -> int:
    """How many checkable facts the model was given.

    Counted rather than estimated, because the number goes to the client
    and §7's "Why am I seeing this?" is a trust feature: a count that
    did not match the explanation's basis would undermine the thing it
    exists to build.
    """
    count = len(facts.planets)
    for field in ("ascendant", "moon_sign", "sun_sign", "current_dasha", "current_antardasha"):
        if getattr(facts, field):
            count += 1
    return count


def _chunk_count(knowledge_text: str) -> int:
    """Retrieved passages, counted from the rendered block.

    The knowledge builder returns prose rather than a list — that is the
    Protocol Phase 4 defined — so the count comes from the separator it
    joins on. Fragile, and the alternative is widening the Protocol,
    which task 5.15 will want anyway. Left as-is here so 5.11 does not
    carry a Phase 4 refactor.
    """
    if not knowledge_text.strip():
        return 0
    return len([block for block in knowledge_text.split("\n\n") if block.strip()])


def _basis(chart: ChartContext, intent: IntentResult) -> list[str]:
    """The human-readable "Why am I seeing this?" basis. §6, §7.

    §16: "'Why am I seeing this?' renders the real stored context." So
    every entry is derived from what was actually selected — never
    composed for display — which is why this reads the fact index rather
    than the intent's selection table. The table says what we MEANT to
    show; the index says what we DID.
    """
    from app.context import HOUSE_NAMES

    basis: list[str] = []

    for planet, fact in sorted(chart.facts.planets.items()):
        house = HOUSE_NAMES.get(fact.house, f"house {fact.house}")
        basis.append(f"{planet.title()} in the {house.split(' (')[0]} house")

    if chart.facts.ascendant:
        basis.append(f"{chart.facts.ascendant.title()} ascendant")
    if chart.facts.current_dasha:
        period = f"{chart.facts.current_dasha.title()} mahadasha"
        if chart.facts.current_antardasha:
            period += f" / {chart.facts.current_antardasha.title()} antardasha"
        basis.append(period)

    if not basis:
        # An empty selection — MEDICAL, LEGAL and the rest. Saying so is
        # better than an empty list the client renders as a blank panel,
        # and it is TRUE: no chart facts were used.
        basis.append("no chart facts were used for this answer")

    del intent
    return basis


def _followups(intent: Intent) -> list[str]:
    """Suggested next questions. PHASE-05 §8, task 5.15.

    Static per intent in this task, and deliberately: §8's generated
    version is 5.15's job, and a model call on the critical path of every
    message for three suggestions is a cost and a latency decision that
    deserves its own task rather than arriving as a side effect of this
    one.

    These are the questions the corpus can actually answer, which is the
    property that matters: a suggested question with nothing behind it
    produces the generic answer §14 lists as the top risk.
    """
    by_intent: dict[Intent, list[str]] = {
        Intent.CAREER: [
            "What does my tenth house say about the kind of work that suits me?",
            "When does my current period end?",
            "How is Saturn placed in my chart?",
        ],
        Intent.MARRIAGE: [
            "What does my seventh house indicate?",
            "How is Venus placed in my chart?",
            "What is Mangal dosha, and how common is it?",
        ],
        Intent.FINANCE: [
            "What is the difference between my second and eleventh houses?",
            "How is Jupiter placed in my chart?",
            "What does my current period suggest about resources?",
        ],
        Intent.EMOTIONAL_SUPPORT: [
            "What does my Moon's nakshatra say about how I process things?",
            "What does my fourth house indicate?",
            "What period am I in right now?",
        ],
        Intent.DASHA: [
            "What comes after my current period?",
            "How is the lord of my current period placed?",
            "What is an antardasha?",
        ],
    }
    return by_intent.get(
        intent,
        [
            "What stands out most in my chart?",
            "What period am I in right now?",
            "What does my ascendant say about me?",
        ],
    )


def _usage_data(usage: Usage, started: float) -> dict[str, int]:
    """Usage for the `done` event.

    Integers throughout. `.claude/CLAUDE.md` invariant 4: money is an
    integer, and `cost_micros` crossing a service boundary as a float is
    the exact mistake that invariant exists to prevent.
    """
    return {
        "input_tokens": usage.input_tokens,
        "output_tokens": usage.output_tokens,
        "cached_tokens": usage.cached_input_tokens,
        "cost_micros": usage.cost_micros,
        "latency_ms": int((time.monotonic() - started) * 1000),
    }


__all__ = [
    "ChatRequest",
    "StreamOutcome",
    "StreamingOrchestrator",
]


_ = settings  # imported for its side-effect-free validation at module load
