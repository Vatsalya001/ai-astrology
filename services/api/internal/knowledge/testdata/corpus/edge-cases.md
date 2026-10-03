---
{
  "title": "Chunker Edge Cases",
  "category": "editorial",
  "source": "editorial",
  "language": "en",
  "authority": 10,
  "metadata": { "topic": ["testing"] }
}
---

This document exists to pin the chunker's behaviour on the inputs that
would otherwise only appear in a real corpus months from now. It is not
astrology.

Decimals must not split a sentence: the Moon moves approximately 13.2
degrees per day, and Ketu sits 180.0 degrees from Rahu. Abbreviations are
the other case, e.g. this clause, i.e. this one, and references to Dr.
Raman's tables. Something like etc. mid-sentence must also hold together.

Transliterated Sanskrit is charged as words rather than as letters:
Viśākhā, Bṛhat Parāśara Horā Śāstra, Pūrvāṣāḍhā.

## A table must stay whole

| Planet | House | Reading |
|---|---|---|
| Saturn | 10 | Authority, slowly |
| Jupiter | 9 | Teaching, travel |
| Mars | 3 | Courage, siblings |

## A code fence must stay whole

```
# this hash is not a heading
weight = vector * 0.6 + keyword * 0.4

# neither is this one
```

## A lead-in paragraph glued to its list

Occupations traditionally associated with this placement include:
- Administration, law and the judiciary
- Research requiring decades
  rather than quarters
- Institutions of long standing

1947. A year opening a paragraph is prose, not list item one thousand nine
hundred and forty-seven, and treating it as a list makes the whole
paragraph one unsplittable unit.

## A heading with no prose under it

## A paragraph after the empty section

A section containing only a heading produces no chunk, because a chunk that
is nothing but a title embeds to something plausible and then competes with
passages that have content.
