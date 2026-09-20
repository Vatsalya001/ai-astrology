"""Reading JSON back from a model that was asked for JSON.

── Why this is one module and not a method on each classifier ──

It was two copies, and they drifted — in the worst possible direction.

`IntentClassifier._parse` was hardened after llama3.2:3b was observed
wrapping its answer in a SINGLE backtick: `` `{"primary": ...}` ``. The
fix — strip backticks unconditionally — was applied there and not to
`SafetyClassifier._parse`, which kept an older guard that only fired on
a TRIPLE fence.

So the recoverable path (a bad intent falls back to broad context) got
the robust parser, and the unrecoverable one did not: an unparseable
safety verdict becomes `none`, the crisis branch never fires, and the
message reaches full astrology generation. Same provider, same `fast`
tier, same wire format — one parser handled the documented quirk and the
safety-critical one did not.

Two copies of a parser will drift again. One will not.

── What it tolerates, and why each one ──

Local models wrap structured output despite being told not to, and each
one does it differently. Every case below was observed or is a trivial
variant of one that was:

  ```json\\n{...}\\n```   a fenced block with a language tag
  ```\\n{...}\\n```       a bare fence
  `{...}`                 a single backtick — llama3.2:3b
  {...} with prose        a model that explains before answering

The last is the only one that needs more than stripping: the object is
extracted by brace matching rather than by a regex, because a regex for
balanced braces does not exist and a greedy `\\{.*\\}` swallows trailing
prose into the JSON.
"""

from __future__ import annotations

import json
from typing import Any


def _strip_wrappers(text: str) -> str:
    """Remove fences, backticks and a language tag, in any combination."""
    stripped = text.strip()

    # `strip("`")` removes any run of backticks from both ends at once,
    # so a triple fence and a single backtick take the same path. Doing
    # it before the language tag matters: ```json opens with the ticks.
    stripped = stripped.strip("`").strip()

    lowered = stripped.lower()
    for tag in ("json", "javascript"):
        if lowered.startswith(tag):
            stripped = stripped[len(tag) :].strip()
            break

    return stripped


def _first_json_object(text: str) -> str | None:
    """The first balanced `{...}`, ignoring braces inside strings.

    For the model that writes "Sure, here is the classification: {...}
    Let me know if you need anything else." A regex cannot do this —
    balanced braces are not a regular language — and `\\{.*\\}` greedily
    swallows the trailing sentence, producing a parse error that reads
    as "the model failed" when it answered correctly.
    """
    start = text.find("{")
    if start == -1:
        return None

    depth = 0
    in_string = False
    escaped = False

    for index in range(start, len(text)):
        char = text[index]

        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if char == '"':
            in_string = not in_string
            continue
        if in_string:
            continue

        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[start : index + 1]

    # Unbalanced — a truncated response. Returning None rather than the
    # partial text so the caller's fallback fires with an honest reason
    # instead of a JSONDecodeError three frames up.
    return None


def parse_json_object(text: str) -> dict[str, Any] | None:
    """A model's reply as a dict, or `None` if it is not one.

    `None` rather than an exception, at every call site that uses this:
    an unparseable classification is recoverable — the caller falls back
    — and raising would turn a slightly-worse answer into no answer at
    the top of the pipeline.

    A top-level LIST is rejected rather than unwrapped. A model that
    returned `[{...}]` was answering a different question than the one
    asked, and quietly taking element zero would hide that.
    """
    candidate = _strip_wrappers(text)
    if not candidate:
        return None

    for attempt in (candidate, _first_json_object(candidate)):
        if attempt is None:
            continue
        try:
            payload = json.loads(attempt)
        except (json.JSONDecodeError, ValueError):
            continue
        if isinstance(payload, dict):
            return payload

    return None
