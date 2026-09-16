/**
 * Birth date and time validation, client side.
 *
 * The server validates too, and the server is the one that counts. This
 * exists so a typo is caught before a round trip on the screen with the
 * highest drop-off in the product — not as a security boundary.
 *
 * Pure functions with no React in them, so the awkward cases can be
 * tested directly rather than through a rendered form. The awkward cases
 * are the point: 31 February, 29 February in 1900 (not a leap year) and
 * in 2000 (one), and a `new Date()` constructor that silently rolls a
 * bad day into the next month instead of rejecting it.
 */

export type DateFailure = 'dateInvalid' | 'dateFuture' | 'dateTooOld'
export type TimeFailure = 'timeInvalid'

export type Validated<T, F> = { ok: true; value: T } | { ok: false; reason: F }

/**
 * Earliest year accepted.
 *
 * The ephemeris kernel starts in mid-1899, so a 1898 birth cannot be
 * computed at all. Refusing it here gives a sentence; letting it through
 * gives a 500 from a service the user has never heard of.
 */
export const MIN_YEAR = 1900

/**
 * Validates a calendar date given as three separate fields.
 *
 * The rollover trap: `new Date(1994, 1, 31)` is 3 March, not an error.
 * JavaScript normalises out-of-range components silently, so a form that
 * trusts the constructor accepts 31 February and stores 3 March — a
 * chart computed for a day the user did not name, with nothing anywhere
 * to show it happened. The three-way read-back below is what catches it.
 */
export function validateBirthDate(
  day: string,
  month: string,
  year: string,
): Validated<string, DateFailure> {
  const d = Number(day)
  const m = Number(month)
  const y = Number(year)

  if (!day || !month || !year || !Number.isInteger(d) || !Number.isInteger(m) || !Number.isInteger(y)) {
    return { ok: false, reason: 'dateInvalid' }
  }
  if (d < 1 || d > 31 || m < 1 || m > 12) {
    return { ok: false, reason: 'dateInvalid' }
  }
  if (y < MIN_YEAR) {
    return { ok: false, reason: 'dateTooOld' }
  }

  // UTC, not local: `new Date(1994, 7, 17)` is midnight in whatever zone
  // the browser happens to be in, and west of Greenwich that is the
  // 16th in UTC. The date the user typed is a calendar date, not an
  // instant, and must not move because of where they are sitting.
  const candidate = new Date(Date.UTC(y, m - 1, d))

  if (
    candidate.getUTCFullYear() !== y ||
    candidate.getUTCMonth() !== m - 1 ||
    candidate.getUTCDate() !== d
  ) {
    // The rollover. 31 February read back as 3 March.
    return { ok: false, reason: 'dateInvalid' }
  }

  // Compared against today's calendar date in UTC, for the same reason.
  const now = new Date()
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  if (candidate.getTime() > today) {
    return { ok: false, reason: 'dateFuture' }
  }

  return {
    ok: true,
    value: `${String(y).padStart(4, '0')}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`,
  }
}

/**
 * Validates a 24-hour clock time given as two fields.
 *
 * Midnight is 00:00 and is valid — a common off-by-one is to treat an
 * empty-looking "0" as missing, which rejects everyone born in the first
 * hour of the day.
 */
export function validateBirthTime(hour: string, minute: string): Validated<string, TimeFailure> {
  if (!hour || !minute) {
    return { ok: false, reason: 'timeInvalid' }
  }

  const h = Number(hour)
  const m = Number(minute)

  if (!Number.isInteger(h) || !Number.isInteger(m)) {
    return { ok: false, reason: 'timeInvalid' }
  }
  if (h < 0 || h > 23 || m < 0 || m > 59) {
    return { ok: false, reason: 'timeInvalid' }
  }

  return { ok: true, value: `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}` }
}
