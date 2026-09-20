"""Tokens to money, in integers, with no silent zero.

── Why this is not in the adapters ──

`OpenAICompatibleProvider` serves five backends whose prices differ by
two orders of magnitude, so an adapter cannot know what a call cost. And
a price changes without a single line of code changing, which makes a
rate constant in an adapter a bill that goes quietly wrong on the day a
vendor updates a page. Adapters report tokens; this converts them.

── Why an unknown model raises ──

The tempting default is zero, and it is the expensive one. A model added
in an admin override and never priced would run for months costing
nothing according to the dashboard, and the discrepancy would first
appear on an invoice nobody can reconcile. `.claude/CLAUDE.md`: money is
an integer and the ledger is append-only — a cost that was recorded wrong
cannot be edited, only corrected with an opposing entry, so recording it
wrong is expensive in a way a missing row is not.

Genuinely free models carry explicit zeros in `pricing.json`. "Free" and
"unpriced" are different facts and this module keeps them different.

── Why integer arithmetic throughout ──

`.claude/CLAUDE.md`, invariant 4. A float cost is summed across millions
of calls, and float addition does not associate: the same charges in a
different order give a different total. Rates are micro-USD per MILLION
tokens, so the division happens exactly once, at the end, on an integer.
"""

from __future__ import annotations

import json
from pathlib import Path

from pydantic import BaseModel, Field

from app.providers.base import Usage

PRICING_FILE = Path(__file__).parent / "pricing.json"

_PER_MILLION = 1_000_000


class UnpricedModelError(KeyError):
    """A model with no entry in `pricing.json`.

    Its own type so the orchestrator can fail the request loudly rather
    than logging a zero. The recovery is a one-line edit to a committed
    JSON file, which is a far better outcome than discovering the gap on
    a bill.
    """


class ModelPrice(BaseModel):
    """Micro-USD per million tokens, per token class.

    `ge=0` and no upper bound: a price of zero is meaningful (local
    models) and a very large one is possible (a future frontier tier),
    but a negative price is always a transcription error and should fail
    at load rather than produce a credit.
    """

    input: int = Field(ge=0)
    output: int = Field(ge=0)
    cache_write: int = Field(ge=0)
    cache_read: int = Field(ge=0)


def _load() -> dict[str, ModelPrice]:
    """Flatten `{vendor: {model: price}}` into one model-keyed table.

    Flat because the caller has a model name from the RESPONSE and not
    always a vendor — OpenRouter reroutes, so the provider that answered
    is not always the vendor whose price applies. Keys beginning with `_`
    are commentary; JSON has no comment syntax and the alternative is
    undocumented magic numbers.
    """
    raw: dict[str, object] = json.loads(PRICING_FILE.read_text())
    table: dict[str, ModelPrice] = {}

    for vendor, models in raw.items():
        if vendor.startswith("_") or not isinstance(models, dict):
            continue
        for model, price in models.items():
            if model.startswith("_") or not isinstance(price, dict):
                continue
            if model in table:
                raise ValueError(
                    f"model {model!r} is priced twice in pricing.json. Two prices for "
                    f"one name means the cost recorded depends on dict ordering, which "
                    f"is not a thing a bill may depend on."
                )
            table[model] = ModelPrice.model_validate(price)

    return table


PRICES: dict[str, ModelPrice] = _load()


def _micros(tokens: int, per_million: int) -> int:
    """One token class, rounded half-up.

    Half-up rather than the truncation `//` would give. Truncation loses
    up to one micro-unit on every single call and always in the same
    direction, which turns noise into a systematic understatement of what
    this service costs — and the whole reason to track cost is to notice
    when it grows.
    """
    return (tokens * per_million + _PER_MILLION // 2) // _PER_MILLION


def cost_micros(model: str, usage: Usage) -> int:
    """What one call cost, in micro-USD.

    Note `input_tokens` is the FRESH input only. Anthropic reports the
    three input classes disjointly and `Usage` preserves that, so adding
    the cached counts here is correct rather than double-counting — and
    the price differs per class by a factor of about twelve, which is the
    entire point of caching.
    """
    try:
        price = PRICES[model]
    except KeyError:
        raise UnpricedModelError(
            f"no price for model {model!r}. Add it to app/pricing.json rather than "
            f"letting it default to zero — an unpriced model bills nothing on the "
            f"dashboard and something real on the invoice, and the ledger this feeds "
            f"is append-only, so a wrong cost is corrected with an opposing entry "
            f"rather than edited away. Local and free models take explicit zeros."
        ) from None

    return (
        _micros(usage.input_tokens, price.input)
        + _micros(usage.output_tokens, price.output)
        + _micros(usage.cache_write_input_tokens, price.cache_write)
        + _micros(usage.cached_input_tokens, price.cache_read)
    )


def priced(model: str, usage: Usage) -> Usage:
    """`usage` with `cost_micros` filled in.

    Returns a copy. Mutating the adapter's object in place would make the
    cost depend on whether anything else had already read it, and the
    telemetry envelope is assembled from several layers.
    """
    return usage.model_copy(update={"cost_micros": cost_micros(model, usage)})


def is_priced(model: str) -> bool:
    """Whether a model can be costed at all.

    Exposed for the admin config endpoint, so an operator changing a
    model mapping learns it is unpriced at the moment they change it
    rather than on the first request afterwards.
    """
    return model in PRICES
