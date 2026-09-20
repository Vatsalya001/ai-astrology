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
import pathlib
import sys
import time
from collections import Counter

from app.classification import IntentClassifier, classify_by_keywords, min_confidence
from app.providers import ModelMap, OpenAICompatibleProvider
from app.settings import settings

DATASET = pathlib.Path(__file__).parent.parent / "tests" / "fixtures" / "intents.jsonl"


async def main() -> int:
    model = sys.argv[1] if len(sys.argv) > 1 else settings.llm_model_fast
    limit = int(sys.argv[2]) if len(sys.argv) > 2 else 0

    rows = [json.loads(line) for line in DATASET.read_text().splitlines() if line.strip()]
    prepass = [(r, classify_by_keywords(r["message"])) for r in rows]
    prepass_correct = sum(1 for r, hit in prepass if hit and hit.primary.value == r["intent"])
    deferred = [r for r, hit in prepass if hit is None]
    if limit:
        deferred = deferred[:limit]

    provider = OpenAICompatibleProvider(
        base_url=settings.llm_base_url,
        api_key=settings.llm_api_key,
        tier="local",
        models=ModelMap(fast=model, chat=model, deep=model),
        # Generous: the point is to finish, not to measure latency.
        timeout_seconds=300.0,
    )
    classifier = IntentClassifier(provider, use_keywords=False)

    raw_correct = final_correct = 0
    model_answers = 0
    lost_to_threshold = 0
    sources: Counter[str] = Counter()
    discarded_confidences: list[float] = []
    examples: list[str] = []

    print(
        f"model={model}  deferred={len(deferred)}  (pre-pass already answered "
        f"{len(rows) - len(deferred)} correctly)\n",
        flush=True,
    )

    started = time.monotonic()
    for index, row in enumerate(deferred, 1):
        result = await classifier.classify(row["message"], trace_id=f"diag-{row['id']}")
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

    if not limit:
        shipped = prepass_correct + final_correct
        ceiling = prepass_correct + raw_correct
        print(f"\n{'=' * 66}")
        print("AS SHIPPED, WHOLE SET")
        print(f"{'=' * 66}")
        print(
            f"  {shipped}/{total} = {shipped / total:.1%}   "
            f"{'MEETS' if shipped / total >= 0.85 else 'BELOW'} the 85% gate"
        )
        print(
            f"  ceiling if the threshold discarded nothing: "
            f"{ceiling}/{total} = {ceiling / total:.1%}"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
