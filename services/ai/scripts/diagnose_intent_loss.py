"""Where the classifier's accuracy actually goes.

`measure_intent_accuracy.py` reports the whole picture and takes two
passes over the set. This one answers a single question on the messages
that reach the model, so it finishes on a loaded machine:

    Is the model WRONG, or is our policy discarding answers it got RIGHT?

Three things discard a classification before it is scored — an
unparseable reply, a provider error, and the confidence threshold — and
the third silently converts a correct answer into a miss whenever the
model is right but unsure. Post-policy accuracy alone cannot tell the
two apart, and they have completely different fixes: one needs a better
model, the other needs one number in app/classification/intents.py.

    uv run python -m scripts.diagnose_intent_loss <model> [limit]
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import sys
import time
from collections import Counter, deque

from app.classification import IntentClassifier, classify_by_keywords, min_confidence
from app.providers import describe, provider_from_settings
from app.settings import settings

DATASET = pathlib.Path(__file__).parent.parent / "tests" / "fixtures" / "intents.jsonl"

# Tokens per minute this run is allowed to spend, 0 for unpaced.
#
# Free hosted tiers meter TOKENS, not requests, and the difference is the
# whole problem. Groq's free tier allows 1000 requests but only 8000
# tokens/minute; one classification of this prompt costs ~1400, so the
# sixth call in a minute is a 429 while 994 of the request budget sits
# unused. Unpaced, that produced a 118-message run in 90 seconds where
# the model answered 3 times and errored 115.
#
# PACING THIS IS NECESSARY AND NOT SUFFICIENT. The per-minute number is
# what the `x-ratelimit-*` response headers advertise, and it is not the
# binding limit: the same tier also caps TOKENS PER DAY at 200000, which
# the headers never mention and which only appears in the body of the
# 429 that finally fires. At ~1400/call that is ~142 calls a day — one
# clean 118-message run, with little spare.
#
# So a paced run can still die two thirds of the way through on a
# budget it could not see. That is what the contamination check at the
# bottom of `main` is for: pacing keeps a run alive, the check keeps a
# dead one from being quoted.
TOKENS_PER_MINUTE = int(os.environ.get("MEASURE_TOKENS_PER_MINUTE", "0"))


class TokenPacer:
    """Sleeps just enough to stay inside a tokens-per-minute budget.

    Adaptive rather than a fixed delay: it charges what each call
    ACTUALLY cost, so a run does not spend twenty minutes pacing for a
    worst case that never happens.

    Not a general-purpose limiter — it assumes one caller in one process
    and a rolling 60-second window, which is what this script is.
    """

    WINDOW = 60.0

    def __init__(self, tokens_per_minute: int) -> None:
        self._budget = tokens_per_minute
        self._spent: deque[tuple[float, int]] = deque()
        # Seeds the first call's estimate. Replaced by the real running
        # mean as soon as anything has been measured.
        self._estimate = 1400

    def charge(self, tokens: int) -> None:
        if not self._budget or tokens <= 0:
            return
        self._spent.append((time.monotonic(), tokens))
        self._estimate = (self._estimate + tokens) // 2

    async def wait(self) -> float:
        """Block until the next call fits. Returns seconds slept."""
        if not self._budget:
            return 0.0

        slept = 0.0
        while True:
            now = time.monotonic()
            while self._spent and now - self._spent[0][0] >= self.WINDOW:
                self._spent.popleft()

            used = sum(tokens for _, tokens in self._spent)
            if used + self._estimate <= self._budget or not self._spent:
                return slept

            # Sleep until the oldest charge ages out of the window.
            pause = self.WINDOW - (now - self._spent[0][0]) + 0.25
            await asyncio.sleep(pause)
            slept += pause


async def main() -> int:
    model = sys.argv[1] if len(sys.argv) > 1 else settings.llm_model_fast
    limit = int(sys.argv[2]) if len(sys.argv) > 2 else 0

    rows = [json.loads(line) for line in DATASET.read_text().splitlines() if line.strip()]
    prepass = [(r, classify_by_keywords(r["message"])) for r in rows]
    prepass_correct = sum(1 for r, hit in prepass if hit and hit.primary.value == r["intent"])
    deferred = [r for r, hit in prepass if hit is None]
    if limit:
        deferred = deferred[:limit]

    provider = provider_from_settings(model_override=model)
    classifier = IntentClassifier(provider, use_keywords=False)

    raw_correct = final_correct = 0
    model_answers = 0
    lost_to_threshold = 0
    sources: Counter[str] = Counter()
    discarded_confidences: list[float] = []
    examples: list[str] = []

    # Naming what this is ACTUALLY talking to, not what the argument
    # said. The scripts used to hardcode an OpenAI-compatible provider,
    # so with LLM_PROVIDER=google they would report a Gemini heading
    # over numbers measured against localhost:11434.
    # `prepass_correct`, NOT `len(rows) - len(deferred)`. The latter is
    # how many the pre-pass ANSWERED, which only equals how many it got
    # RIGHT while its precision is 100% — and it silently becomes
    # nonsense under `limit`, where it subtracts the TRUNCATED deferred
    # count and reports the whole remainder as correct. Run with a limit
    # of 3 it printed "pre-pass already answered 197 correctly" against
    # a set of 200.
    answered_by_prepass = len(rows) - sum(1 for _, hit in prepass if hit is None)
    # The PROMPT version, read off the constructed classifier rather than
    # off settings. This script ran a full 118-message measurement
    # against v2 while the service ran v3, because IntentClassifier's
    # default was a hardcoded "v2" and this file did not pass one. The
    # number was going into the gate report as "as shipped".
    print(
        f"  {describe(model_override=model)}  "
        f"prompt=intent_classification.{classifier._prompt_version}",
        flush=True,
    )
    print(
        f"  deferred={len(deferred)}"
        f"{'  (LIMITED — the totals below are not the gate number)' if limit else ''}\n"
        f"  pre-pass answered {answered_by_prepass}/{len(rows)}, "
        f"of which {prepass_correct} correct\n",
        flush=True,
    )

    started = time.monotonic()
    pacer = TokenPacer(TOKENS_PER_MINUTE)
    for index, row in enumerate(deferred, 1):
        await pacer.wait()
        result = await classifier.classify(row["message"], trace_id=f"diag-{row['id']}")

        # One retry on a provider error, after a full window. A 429 is
        # the pacer's estimate having been too low, not a property of
        # the model — and counting it as a failed classification is how
        # the unpaced run reported a rate limit as a 56% accuracy.
        if result.source == "provider_error":
            await asyncio.sleep(TokenPacer.WINDOW + 1 if TOKENS_PER_MINUTE else 0)
            result = await classifier.classify(row["message"], trace_id=f"diag-{row['id']}r")

        pacer.charge(result.stats.usage.input_tokens + result.stats.usage.output_tokens)
        sources[result.source] += 1

        # What the MODEL said, before policy — or None when there was no
        # answer for a policy to act on.
        #
        # The first version was `result.fallback_from or result.primary`,
        # which silently counted an UNPARSEABLE reply as a raw answer of
        # GENERAL_ASTROLOGY. 27 of the 200 rows carry that label, so a
        # model returning nothing useful scored "raw correct" by luck —
        # inflating the exact number this script exists to establish, in
        # the direction that would have made the threshold look guilty.
        answered = result.source in ("model", "low_confidence")
        raw = (result.fallback_from or result.primary) if answered else None

        raw_right = raw is not None and raw.value == row["intent"]
        final_right = result.primary.value == row["intent"]

        if answered:
            model_answers += 1
            raw_correct += raw_right
        final_correct += final_right

        if result.source == "low_confidence" and raw is not None:
            discarded_confidences.append(result.fallback_confidence)
            if raw_right:
                lost_to_threshold += 1
                if len(examples) < 8:
                    examples.append(
                        f"    {row['id']}  {row['message'][:44]:<44} "
                        f"model said {raw.value} @ "
                        f"{result.fallback_confidence} — CORRECT, discarded"
                    )

        if index % 10 == 0:
            rate = (time.monotonic() - started) / index
            print(
                f"  {index}/{len(deferred)}  raw={raw_correct} final={final_correct}  "
                f"({rate:.0f}s/call, ~{rate * (len(deferred) - index) / 60:.0f} min left)",
                flush=True,
            )

    total = len(rows)
    print(f"\n{'=' * 66}")
    print(f"ON THE {len(deferred)} MESSAGES THAT REACH THE MODEL")
    print(f"{'=' * 66}")
    n = len(deferred)
    print(
        f"  the model ANSWERED {model_answers}/{n} times "
        f"({n - model_answers} unparseable or failed at the provider)"
    )
    if model_answers:
        print(
            f"  of those, right    {raw_correct}/{model_answers} = "
            f"{raw_correct / model_answers:.1%}   <- the MODEL's accuracy"
        )
    print(
        f"  after our policy   {final_correct}/{n} = {final_correct / n:.1%}"
        f"   <- what the product delivers"
    )
    print(
        f"  CORRECT answers discarded by the threshold: {lost_to_threshold}"
        f"  (MIN_CONFIDENCE = {min_confidence()})"
    )
    print(f"  source breakdown: {dict(sources)}")
    if discarded_confidences:
        discarded_confidences.sort()
        print(
            f"  discarded confidences: min {min(discarded_confidences)}, "
            f"median {discarded_confidences[len(discarded_confidences) // 2]}, "
            f"max {max(discarded_confidences)}"
        )
    if examples:
        print("\n  right answers the threshold threw away:")
        print("\n".join(examples))

    # A run that lost calls to the PROVIDER did not measure the model,
    # and must not print a gate number as though it had.
    #
    # The failure this exists to stop has now happened twice. Unpaced,
    # 115 of 118 calls 429'd and the script reported "56.0%" — a rate
    # limit wearing the shape of an accuracy, because a provider error
    # falls back to GENERAL_ASTROLOGY and 27 of the 200 rows carry that
    # label, so failing scores points. Paced to Groq's PER-MINUTE limit,
    # 48 still failed: the binding limit was 200000 tokens per DAY, and
    # the run exhausted it partway through. It printed 80.5% while the
    # model was answering 92.9% of what it was actually asked.
    #
    # The tell both times was the ceiling printing BELOW the shipped
    # figure, which is arithmetically impossible unless rows never
    # reached the model. That is now checked rather than left for a
    # reader to notice.
    failed = sources["provider_error"]
    contaminated = failed > max(2, len(deferred) // 20)

    if contaminated:
        print(f"\n{'!' * 66}")
        print(f"  NOT A MEASUREMENT — {failed}/{len(deferred)} calls failed at the provider.")
        print("  Those rows fell back to GENERAL_ASTROLOGY, which is a LABEL IN THE")
        print("  SET, so provider failures score points and the totals below read")
        print("  higher than the model earned. Do not quote them.")
        if model_answers:
            sound = f"{raw_correct}/{model_answers} = {raw_correct / model_answers:.1%}"
            print(f"\n  The model's accuracy on what it was actually asked — {sound} —")
            print("  is the only sound number here. Re-run when quota allows.")
        else:
            # Nothing got through, so there is no sound number at all.
            # Dividing here is how this banner crashed the first time it
            # fired for real: `llama3.2:3b` passed while LLM_PROVIDER
            # pointed at Groq, every call 404'd, and model_answers was 0.
            print("\n  NOTHING reached the model, so there is no accuracy here at all.")
            print("  Check the provider/model line at the top: a model name from one")
            print("  vendor sent to another is a 404 on every row.")
        print(f"{'!' * 66}")

    if not limit:
        shipped = prepass_correct + final_correct
        ceiling = prepass_correct + raw_correct
        print(f"\n{'=' * 66}")
        print("AS SHIPPED, WHOLE SET" + ("  (INVALID — see above)" if contaminated else ""))
        print(f"{'=' * 66}")
        print(
            f"  {shipped}/{total} = {shipped / total:.1%}   "
            f"{'MEETS' if shipped / total >= 0.85 else 'BELOW'} the 85% gate"
        )
        print(
            f"  ceiling if the threshold discarded nothing: "
            f"{ceiling}/{total} = {ceiling / total:.1%}"
        )
        if ceiling < shipped:
            print(
                "\n  NOTE: the ceiling is BELOW the shipped figure. That is only\n"
                "  possible when rows never reached the model, so the shipped\n"
                "  figure is inflated by lucky fallbacks rather than earned."
            )

    # Non-zero so a CI step or a shell `&&` cannot treat a contaminated
    # run as a result.
    return 1 if contaminated else 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
