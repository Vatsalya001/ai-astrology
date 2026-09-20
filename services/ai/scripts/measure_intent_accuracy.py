"""Run the 200-message set through a real model and report accuracy.

    uv run python -m scripts.measure_intent_accuracy

PHASE-04 §12 wants ≥85% primary-intent accuracy; §13 says the run is
"the 200-message set against the local free model". Free, repeatable, and
— crucially — the thing that lets a model be swapped later with evidence
rather than hope.

── Why this is not a test ──

`.claude/rules/testing.md`: CI never calls a model. A pytest module is
something CI collects by default, so the only reliable way to keep 200
model calls out of a CI run is for this not to be one.

The offline half — the keyword pre-pass, which is pure code — IS a test,
in `tests/test_intent_dataset.py`, and it runs on every commit.

── Three numbers, because one would hide the interesting part ──

  overall      what the user experiences
  model only   the classifier's own quality, pre-pass off
  pre-pass     coverage and precision, which is the cost saving

Reporting only the first makes a strong pre-pass paper over a weak model,
which is exactly the state in which swapping the model looks safe and is
not.
"""

from __future__ import annotations

import asyncio
import json
from collections import Counter
from pathlib import Path

from app.classification import Intent, IntentClassifier, classify_by_keywords
from app.providers import ModelMap, OpenAICompatibleProvider
from app.settings import settings

DATASET = Path(__file__).parent.parent / "tests" / "fixtures" / "intents.jsonl"
TARGET = 0.85


def load() -> list[dict[str, str]]:
    return [json.loads(line) for line in DATASET.read_text().splitlines() if line.strip()]


async def score(classifier: IntentClassifier, rows: list[dict[str, str]]) -> tuple[int, list[str]]:
    correct = 0
    confusions: list[str] = []

    # Sequential rather than gathered. Ollama on a laptop serves one
    # request at a time regardless; firing 200 at once only fills a queue
    # and makes the first timeout look like a model failure.
    for row in rows:
        result = await classifier.classify(row["message"], trace_id=f"acc-{row['id']}")
        if result.primary.value == row["intent"]:
            correct += 1
        else:
            confusions.append(
                f"  {row['id']}  {row['message'][:52]:<52} "
                f"want {row['intent']:<18} got {result.primary.value}"
            )

    return correct, confusions


def report_pre_pass(rows: list[dict[str, str]]) -> None:
    decided = wrong = 0
    for row in rows:
        hit = classify_by_keywords(row["message"])
        if hit is None:
            continue
        decided += 1
        if hit.primary.value != row["intent"]:
            wrong += 1

    print("─── keyword pre-pass (no model, also asserted in CI) ───")
    print(
        f"  coverage   {decided}/{len(rows)} = {decided / len(rows):.1%} of messages skip the model"
    )
    if decided:
        print(f"  precision  {1 - wrong / decided:.1%}  ({wrong} wrong)")
    print()


async def main() -> int:
    rows = load()

    provider = OpenAICompatibleProvider(
        base_url=settings.llm_base_url,
        api_key=settings.llm_api_key,
        tier=settings.llm_provider_tier,
        models=ModelMap(
            fast=settings.llm_model_fast,
            chat=settings.llm_model_chat,
            deep=settings.llm_model_deep,
        ),
    )

    if not await provider.health_check():
        print(f"{settings.llm_base_url} is not answering. Start Ollama, or point")
        print("LLM_BASE_URL at a free hosted backend — Groq and Cerebras both work")
        print("through this same adapter.")
        return 2

    print(f"model={settings.llm_model_fast}  backend={settings.llm_base_url}")
    print(f"{len(rows)} messages. Target for the gate: {TARGET:.0%} primary-intent accuracy.\n")

    report_pre_pass(rows)

    print("─── model only (pre-pass off) ───")
    model_correct, _ = await score(IntentClassifier(provider, use_keywords=False), rows)
    print(f"  accuracy   {model_correct}/{len(rows)} = {model_correct / len(rows):.1%}\n")

    print("─── as shipped (pre-pass on) ───")
    overall_correct, overall_misses = await score(IntentClassifier(provider), rows)
    overall = overall_correct / len(rows)
    print(f"  accuracy   {overall_correct}/{len(rows)} = {overall:.1%}")
    print(f"  {'MEETS' if overall >= TARGET else 'BELOW'} the {TARGET:.0%} gate\n")

    if overall_misses:
        print("─── misclassified, as shipped ───")
        for line in overall_misses:
            print(line)
        print()

        # Which true intents are being lost, not which were predicted.
        # The predicted-label histogram is the one people reach for and
        # it points at the symptom; this points at the gap.
        lost = Counter(line.split("want ")[1].split()[0] for line in overall_misses)
        print("─── intents most often missed ───")
        for intent, count in lost.most_common(5):
            total = sum(1 for row in rows if row["intent"] == intent)
            print(f"  {intent:<20} {count}/{total} wrong")

    unseen = {intent.value for intent in Intent} - {row["intent"] for row in rows}
    if unseen:
        print(f"\nNot represented in the set at all: {sorted(unseen)}")

    print("\nRecord model, date and accuracy in docs/PROJECT_STATUS.md.")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
