import { describe, expect, it } from 'vitest'

import { DIGNITIES, dignity, formatDegree, formatNakshatra } from './format'

describe('formatDegree', () => {
  it('renders degrees and arcminutes', () => {
    expect(formatDegree(15.2333)).toBe("15°13'")
    expect(formatDegree(8.7)).toBe("8°42'")
    expect(formatDegree(0)).toBe("0°00'")
  })

  it('pads arcminutes to two digits', () => {
    // `5°4'` reads as five degrees four minutes to a person and as a
    // typo to anyone scanning a column of them.
    expect(formatDegree(5.0667)).toBe("5°04'")
    expect(formatDegree(22.05)).toBe("22°03'")
  })

  /**
   * The bug this function exists to not have.
   *
   * A sign spans [0°, 30°). Rounding the arcminutes carries at the top
   * of the sign — 29.9999° is 59.994', which rounds to 60', which has to
   * become `30°00'`. That position does not exist, and it prints at the
   * exact boundary where a planet is about to change sign and a reader
   * is most likely to be checking.
   */
  it('never produces a 30th degree at the top of a sign', () => {
    for (const degree of [29.9999, 29.99999, 29.999999999]) {
      expect(formatDegree(degree), `${degree}`).toBe("29°59'")
    }
  })

  /**
   * Sampled where the carry actually lives.
   *
   * This first stepped by 0.01 across the sign and was vacuous: it stayed
   * green against four different broken implementations, including two
   * that print `30°00'`. A carry only happens in the last arcminute
   * before a whole degree — at 29.99 the remainder is 59.4', which
   * rounds to 59, so a 0.01 grid never reaches the failing region at all.
   *
   * So the samples sit just under every whole degree, which is exactly
   * where a naive `Math.round` of the arcminutes produces 60.
   */
  it('never produces 60 arcminutes, sampled just below every whole degree', () => {
    const offenders: string[] = []

    for (let d = 1; d <= 29; d++) {
      for (const delta of [1e-4, 1e-6, 1e-9]) {
        const formatted = formatDegree(d - delta)
        const minutes = Number(formatted.split('°')[1]!.replace("'", ''))
        if (minutes >= 60) offenders.push(`${d} - ${delta} -> ${formatted}`)
      }
    }

    expect(offenders.slice(0, 5)).toEqual([])
  })

  it('does not round a degree up to the next one', () => {
    // 15.999° is in the 16th degree, not the 17th. Rounding would print
    // `16°00'` and move the planet a whole degree.
    expect(formatDegree(15.999)).toBe("15°59'")
  })

  /**
   * The float trap, enumerated rather than spot-checked.
   *
   * The first version of this function subtracted the whole degrees and
   * multiplied the remainder. That subtraction loses low bits downward,
   * so an exact 8°42' printed as 8°41'. It was wrong on 790 of these
   * 1800 positions — 44% — and every one of them is a value a printed
   * chart actually contains, because charts are quoted to the arcminute.
   *
   * Three passed by luck when this was first written as a spot check.
   */
  it('is exact on all 1800 whole arcminute positions in a sign', () => {
    const wrong: string[] = []

    for (let d = 0; d < 30; d++) {
      for (let m = 0; m < 60; m++) {
        const expected = `${d}°${String(m).padStart(2, '0')}'`
        const actual = formatDegree(d + m / 60)
        if (actual !== expected) wrong.push(`${expected} printed as ${actual}`)
      }
    }

    expect(
      wrong.slice(0, 8),
      `${wrong.length} of 1800 exact arcminute positions are off. This is the ` +
        'subtract-then-multiply float error, not a rounding preference.',
    ).toEqual([])
  })
})

describe('dignity', () => {
  it('renders the closed vocabulary from astro-service', () => {
    expect(dignity('exalted').label).toBe('Exalted')
    expect(dignity('own_sign').label).toBe('Own sign')
    expect(dignity('debilitated').label).toBe('Debilitated')
    expect(dignity('moolatrikona').label).toBe('Moolatrikona')
  })

  it('renders neutral as an em dash with nothing to tap', () => {
    expect(dignity('neutral')).toEqual({ label: '—', term: null })
  })

  // Swallowing an unrecognised value would blank a column the engine had
  // something to say in, and nothing would report it.
  it('shows an unknown dignity rather than hiding it', () => {
    expect(dignity('great_friend')).toEqual({ label: 'great_friend', term: null })
  })

  // Every dignity that IS a term of art must be tappable, or the
  // glossary is decoration. Written as a list so adding a dignity
  // without a definition fails here rather than shipping a dead column.
  it('gives every real dignity a glossary term', () => {
    const missing = Object.entries(DIGNITIES)
      .filter(([key, value]) => key !== 'neutral' && value.term === null)
      .map(([key]) => key)
    expect(missing).toEqual([])
  })
})

describe('formatNakshatra', () => {
  it('includes the pada', () => {
    // The pada selects the navamsa sign. A nakshatra without it is a
    // third of the information.
    expect(formatNakshatra('Rohini', 2)).toBe('Rohini 2')
  })
})
