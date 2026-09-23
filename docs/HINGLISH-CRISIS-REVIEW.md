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
- ~~**Devanagari is not covered at all.**~~ **Partly closed 2026-09-23** — see the
  second table below. The DIRECT forms now match; whether they are the forms people
  actually type is still the question only you can answer.

## The Devanagari phrases, added 2026-09-23

These were added without a native-speaker review, on the narrow grounds that each
one's meaning is dictionary-level rather than idiomatic — `आत्महत्या` means suicide and
nothing else. **That is an argument about translation, not about usage**, and usage is
what this review is for. Review them exactly as harshly as the table above.

The first question matters far more here than anywhere else on this page: until this
change the list matched **nothing** in Devanagari, so there is no reason to think the
fifteen rows below are the right fifteen.

| # | pattern | intended meaning | misses anything? | wrongly catches? |
|---|---|---|---|---|
| D1 | `मरना चाहत` | "(I) want to die" — covers चाहता and चाहती |  |  |
| D2 | `मरना है` | "(I) have to die" |  |  |
| D3 | `जीना नहीं चाहत` | "(I) don't want to live" |  |  |
| D4 | `नहीं जीना` | "(I) don't want to live" — the short form |  |  |
| D5 | `जीने का मन नहीं` | "(I) don't feel like living" |  |  |
| D6 | `जीने की इच्छा नहीं` | same, more formal |  |  |
| D7 | `आत्महत्या` | suicide (Sanskrit-derived) |  |  |
| D8 | `खुदकुशी` | suicide (Urdu-derived) |  |  |
| D9 | `अपनी जान` | "(take) my own life" |  |  |
| D10 | `जान दे` | "give up (my) life" — देना / दूंगा / दूँगी |  |  |
| D11 | `जिंदगी खत्म` | "life over" |  |  |
| D12 | `सब खत्म कर` | "end it all" |  |  |
| D13 | `मेरे बिना (?:सब\|सभी)` | "without me, everyone (would be better off)" |  |  |
| D14 | `मर जाऊ` | "(I) will die" — जाऊं / जाऊँगा |  |  |

### Two things to know before reviewing these

**Nuqta is normalised away.** `ज़िंदगी` and `जिंदगी` are the same string by the time a
pattern sees them, and so are `ख़ुदकुशी` and `खुदकुशी`. Devanagari encodes the nuqta two
ways and a phone keyboard picks one without telling anyone, so do not report a pattern
as missing a spelling that differs only in the dot.

**`मेरे बिना` requires `सब`/`सभी`.** Bare, it flagged `मेरे बिना मत जाओ` — "don't go
without me" — which is an ordinary sentence. The transliterated half of this list has
always had that restriction; the Devanagari half briefly did not, and a negative test
caught it.

## What happens on a match

Zero model calls. The user gets `app/safety/responses/crisis.en.md` — a static,
human-written message pointing at **findahelpline.com**, which resolves a free
confidential line by country. Local numbers were deliberately removed because nobody
had dialled them; see `app/safety/responses/README.md`.
