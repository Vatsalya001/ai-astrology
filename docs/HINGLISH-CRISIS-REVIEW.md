# Hinglish crisis phrases — native-speaker review

**What this is.** `services/ai/app/safety/crisis.py` short-circuits a message to a
static crisis response *before any model is called*. It matches on the phrases below.
This is the highest-stakes list in the repository and **no native Hinglish speaker has
ever read it.**

A test proves a regex matches the sentence we wrote down. It cannot prove that is the
sentence a real person in distress actually writes. That gap is what this review closes.

## How to review

Two questions, and the first matters far more:

1. **What would somebody actually write that this list MISSES?** A miss means a person
   in crisis gets an astrology reading. Add anything you can think of, including
   regional spellings, Devanagari, and the ways people write when they are not
   composing carefully.
2. **What ordinary sentence would this list WRONGLY catch?** A false positive costs
   somebody a mildly annoying helpline message — much cheaper than a miss, so lean
   toward over-matching. Only flag it if the phrase is genuinely common in innocent use.

## The phrases

| # | pattern | intended meaning | misses anything? | wrongly catches? |
|---|---|---|---|---|
| 1 | `marna chahta` | “(I) want to die” — masculine speaker |  |  |
| 2 | `marna chahti` | “(I) want to die” — feminine speaker |  |  |
| 3 | `jeena nahi chahta` | “(I) don’t want to live” — masculine |  |  |
| 4 | `jeena nahi chahti` | “(I) don’t want to live” — feminine |  |  |
| 5 | `jeene ka mann nahi` | “(I) don’t feel like living” |  |  |
| 6 | `khudkushi` | suicide (Urdu-derived) |  |  |
| 7 | `aatmahatya` | suicide (Sanskrit-derived) |  |  |
| 8 | `apni jaan` | “(take) my own life” — matches the fragment only |  |  |
| 9 | `zindagi khatam` | “life over” / “finish life” |  |  |
| 10 | `jaan de d` | “give up (my) life” — dunga / dungi / di |  |  |
| 11 | `jaan dena hai` | “(I) have to give up (my) life” |  |  |
| 12 | `nahin? jeena` | “(I) don’t want to live” — accepts nahi and nahin |  |  |
| 13 | `sab (?:kuch )?khatam kar d` | “end it all” — kar dunga / kar dena |  |  |
| 14 | `sab kuch khatam ho jaye` | “may everything end” |  |  |
| 15 | `mere bina (?:sab|sabhi)` | “without me, everyone (would be better off)” |  |  |

## Notes from the code, for context

- Matching is **case-insensitive** and **substring**, not whole-word — so `apni jaan`
  fires inside a longer sentence.
- `nahin? jeena` deliberately accepts both `nahi` and `nahin`.
- `ho jaye` is kept separate from `ho gaya`, which is the ordinary past tense of
  something simply running out.
- **Devanagari is not covered at all.** If people write मुझे नहीं जीना, none of this
  matches. That is a known, unaddressed gap and the single most useful thing a
  reviewer could tell us about.

## What happens on a match

Zero model calls. The user gets `app/safety/responses/crisis.en.md` — a static,
human-written message pointing at **findahelpline.com**, which resolves a free
confidential line by country. Local numbers were deliberately removed because nobody
had dialled them; see `app/safety/responses/README.md`.
