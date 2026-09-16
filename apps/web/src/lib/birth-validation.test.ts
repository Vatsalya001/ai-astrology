import { describe, expect, it } from 'vitest'

import { MIN_YEAR, validateBirthDate, validateBirthTime } from './birth-validation'

describe('validateBirthDate', () => {
  it('accepts an ordinary date and formats it for the API', () => {
    expect(validateBirthDate('17', '8', '1994')).toEqual({ ok: true, value: '1994-08-17' })
  })

  it('zero-pads, so the server never has to guess', () => {
    expect(validateBirthDate('1', '1', '1990')).toEqual({ ok: true, value: '1990-01-01' })
  })

  // The trap this function exists for. `new Date(1994, 1, 31)` is not an
  // error — it is 3 March. A form that trusts the constructor stores a
  // chart for a day the user never named, and nothing anywhere shows it
  // happened.
  it.each([
    ['31 February', '31', '2'],
    ['30 February', '30', '2'],
    ['31 April', '31', '4'],
    ['31 June', '31', '6'],
    ['31 September', '31', '9'],
    ['31 November', '31', '11'],
  ])('rejects %s instead of rolling it into the next month', (_label, day, month) => {
    expect(validateBirthDate(day, month, '1994')).toEqual({
      ok: false,
      reason: 'dateInvalid',
    })
  })

  // Leap years are the case where "31 days in a month" reasoning and the
  // actual calendar diverge, and 1900 is the century rule almost nobody
  // implements: divisible by 100 but not 400, so NOT a leap year.
  it('accepts 29 February in a leap year', () => {
    expect(validateBirthDate('29', '2', '2000')).toEqual({ ok: true, value: '2000-02-29' })
    expect(validateBirthDate('29', '2', '1996')).toEqual({ ok: true, value: '1996-02-29' })
  })

  it('rejects 29 February in 1900, which was not a leap year', () => {
    expect(validateBirthDate('29', '2', '1900')).toEqual({ ok: false, reason: 'dateInvalid' })
  })

  it('rejects 29 February in an ordinary year', () => {
    expect(validateBirthDate('29', '2', '1999')).toEqual({ ok: false, reason: 'dateInvalid' })
  })

  it('rejects components outside any calendar', () => {
    expect(validateBirthDate('0', '8', '1994').ok).toBe(false)
    expect(validateBirthDate('32', '8', '1994').ok).toBe(false)
    expect(validateBirthDate('17', '0', '1994').ok).toBe(false)
    expect(validateBirthDate('17', '13', '1994').ok).toBe(false)
  })

  it('rejects an empty field rather than treating it as zero', () => {
    expect(validateBirthDate('', '8', '1994').ok).toBe(false)
    expect(validateBirthDate('17', '', '1994').ok).toBe(false)
    expect(validateBirthDate('17', '8', '').ok).toBe(false)
  })

  // The ephemeris kernel starts in mid-1899, so an earlier birth cannot
  // be computed at all. Saying so here is a sentence; letting it through
  // is a 500 from a service the user has never heard of.
  it('rejects a year before the ephemeris kernel begins', () => {
    expect(validateBirthDate('1', '1', String(MIN_YEAR - 1))).toEqual({
      ok: false,
      reason: 'dateTooOld',
    })
    const old = validateBirthDate('1', '1', '1850')
    expect(old.ok).toBe(false)
    if (!old.ok) expect(old.reason).toBe('dateTooOld')
  })

  it('accepts the first year the kernel covers', () => {
    expect(validateBirthDate('1', '1', String(MIN_YEAR)).ok).toBe(true)
  })

  it('rejects a future date', () => {
    const nextYear = new Date().getUTCFullYear() + 1
    expect(validateBirthDate('1', '1', String(nextYear))).toEqual({
      ok: false,
      reason: 'dateFuture',
    })
  })

  // Today must be accepted. Rejecting it would refuse anyone entering a
  // newborn's details, which is a real thing people do.
  it('accepts today', () => {
    const now = new Date()
    const result = validateBirthDate(
      String(now.getUTCDate()),
      String(now.getUTCMonth() + 1),
      String(now.getUTCFullYear()),
    )
    expect(result.ok).toBe(true)
  })
})

describe('validateBirthTime', () => {
  it('accepts an ordinary time', () => {
    expect(validateBirthTime('14', '35')).toEqual({ ok: true, value: '14:35' })
  })

  // A common off-by-one: treating "0" as missing rejects everyone born
  // in the first hour of the day, and midnight is a real birth time.
  it('accepts midnight', () => {
    expect(validateBirthTime('0', '0')).toEqual({ ok: true, value: '00:00' })
  })

  it('accepts the last minute of the day', () => {
    expect(validateBirthTime('23', '59')).toEqual({ ok: true, value: '23:59' })
  })

  it('rejects a 24th hour, which does not exist on a 24-hour clock', () => {
    expect(validateBirthTime('24', '0')).toEqual({ ok: false, reason: 'timeInvalid' })
  })

  it('rejects a 60th minute', () => {
    expect(validateBirthTime('12', '60')).toEqual({ ok: false, reason: 'timeInvalid' })
  })

  it('rejects an empty field rather than defaulting it to zero', () => {
    expect(validateBirthTime('', '35').ok).toBe(false)
    expect(validateBirthTime('14', '').ok).toBe(false)
  })

  it('zero-pads so the server receives HH:MM', () => {
    expect(validateBirthTime('7', '5')).toEqual({ ok: true, value: '07:05' })
  })
})
