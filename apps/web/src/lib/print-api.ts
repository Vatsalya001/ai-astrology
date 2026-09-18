import { api } from './api'

/**
 * The one endpoint reached with a print token instead of a session.
 *
 * ── Why this is not in `astrology-api.ts` ──
 *
 * Everything there goes through `authed()`, which attaches the stored
 * access token. This caller has none: it is headless Chrome on the PDF
 * worker, started fresh with no localStorage and no cookies. Its
 * credential is the single-use token in the URL.
 *
 * Kept in its own file so that distinction is visible. A print fetch
 * added to `astrology-api.ts` would sit next to twenty authenticated
 * ones and be read as another of them.
 *
 * ── One request, because the token permits exactly one ──
 *
 * Redeeming the token deletes it, so there is no second call to make.
 * The API answers with everything the document renders — all three
 * charts, the mahadasha sequence, the current period — in one body.
 */

export interface PrintProfile {
  id: string
  label: string
  birth_date: string
  birth_time: string | null
  time_accuracy: 'exact' | 'approximate' | 'unknown'
  birth_place: string
  timezone: string
}

export interface PrintChart {
  id: string
  birth_profile_id: string
  chart_type: string
  ayanamsa: string
  house_system: string
  engine_version: string
  chart_data: unknown
  computed_at: string
}

export interface PrintPeriod {
  id: string
  planet: string
  start: string
  end: string
  level: number
  elapsed_percent?: number
}

export interface PrintBundle {
  birth_profile: PrintProfile
  /** Keyed by chart type — "D1", "D9", "D10". A divisional may be absent. */
  charts: Record<string, PrintChart | undefined>
  /** Null when the profile has no birth time: no time, no dasha tree. */
  mahadashas: PrintPeriod[] | null
  current_dasha: {
    mahadasha: PrintPeriod | null
    antardasha: PrintPeriod | null
    pratyantardasha: PrintPeriod | null
    at: string
  } | null
  generated_at: string
}

export class PrintTokenRejectedError extends Error {
  constructor() {
    super('This print link is no longer valid.')
    this.name = 'PrintTokenRejectedError'
  }
}

/**
 * Fetches the document's data with a print token.
 *
 * The token is the only argument, and it is the only thing sent. In
 * particular no profile id: the API takes the scope from the token, and
 * a profile id here would be a value asking to be trusted.
 */
export async function fetchPrintBundle(
  token: string,
  signal?: AbortSignal,
): Promise<PrintBundle> {
  const res = await fetch(`${api.url}/api/v1/print/chart?token=${encodeURIComponent(token)}`, {
    signal,
    // No credentials: this request must not pick up a session cookie
    // from a developer's browser and appear to work for a reason it
    // will not work for on the worker.
    credentials: 'omit',
    headers: { Accept: 'application/json' },
  })

  if (res.status === 404) {
    // Expired, already spent, or never real — the API deliberately does
    // not distinguish them, and neither does this.
    throw new PrintTokenRejectedError()
  }
  if (!res.ok) {
    throw new Error(`print bundle request failed with ${res.status}`)
  }

  return (await res.json()) as PrintBundle
}
