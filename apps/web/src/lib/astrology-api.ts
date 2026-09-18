import { authed } from './users-api'
import { DEFAULT_VARGA, type VargaType } from './varga'

/**
 * The Phase 2 endpoints: places, birth profiles, charts.
 *
 * The web app talks only to the Go API and has no idea astro-service
 * exists. That is not incidental — it is what lets the compute service
 * be swapped, scaled or taken down without the browser noticing.
 */

export interface Place {
  id: number
  name: string
  admin1: string
  country_code: string
  latitude: number
  longitude: number
  timezone: string
  population: number
}

export interface BirthProfile {
  id: string
  label: string
  birth_date: string
  /** `null` when time_accuracy is "unknown". */
  birth_time: string | null
  time_accuracy: 'exact' | 'approximate' | 'unknown'
  birth_place: string
  latitude: number
  longitude: number
  timezone: string
  utc_offset_min: number
  utc_instant: string
  version: number
  is_active: boolean
  superseded_by: string | null
  created_at: string
}

export interface CreateProfileInput {
  label?: string
  /** Local calendar date, YYYY-MM-DD. Never a UTC instant. */
  birth_date: string
  /** Local clock time, HH:MM. Omitted when the time is unknown. */
  birth_time?: string
  time_accuracy: 'exact' | 'approximate' | 'unknown'
  place_id: number
}

export interface Chart {
  id: string
  birth_profile_id: string
  chart_type: string
  ayanamsa: string
  house_system: string
  engine_version: string
  chart_data: unknown
  computed_at: string
  /** Set when astro-service was unreachable and this is a stored chart. */
  stale?: boolean
}

export interface DashaPeriod {
  id: string
  planet: string
  /** RFC 3339. */
  start: string
  end: string
  level: number
  parent_id?: string
  elapsed_percent?: number
}

export interface CurrentDashas {
  mahadasha: DashaPeriod | null
  antardasha: DashaPeriod | null
  pratyantardasha: DashaPeriod | null
  at: string
}

export interface TransitPosition {
  planet: string
  sign: string
  sign_index: number
  degree: number
  longitude: number
  is_retrograde: boolean
  timestamp: string
  /** 1..12, counted inclusively from the natal Moon's sign. */
  house_from_moon: number
}

/**
 * Saturn's seven-and-a-half-year passage over the natal Moon.
 *
 * Note what is NOT here: start and end dates. The engine reports the
 * phase and the geometry, not the span, and the UI does not invent one —
 * deriving "ends April 2033" in TypeScript would be the frontend
 * computing astrology, which is the same rule that keeps the model out
 * of it. See `SadeSatiIndicator`.
 */
export interface SadeSatiStatus {
  is_active: boolean
  phase: string | null
  saturn_sign: string
  houses_from_moon: number

  /**
   * When the stretch began and when it ends, computed by the engine.
   *
   * Null when it is not running — and also when it IS running but the
   * server has no stored window yet (a fresh database before the first
   * worker pass). The UI must render that as "we do not know", never as
   * "it has no end": the difference matters to somebody deciding
   * whether to plan around it.
   */
  started_at: string | null
  ends_at: string | null
}

export interface NatalTransits {
  at: string
  natal_moon_sign: string
  transits: TransitPosition[]
  sade_sati: SadeSatiStatus
}

/**
 * Minimum characters before a search is worth making.
 *
 * Matches the server, which returns an empty list below this rather than
 * an error — a user mid-word has not made a mistake. Duplicating the
 * number here saves a round trip per keystroke for the first character,
 * which on a slow connection is the difference between a search box that
 * feels instant and one that does not.
 */
export const MIN_QUERY_LENGTH = 2

/**
 * How long to wait after the last keystroke.
 *
 * 250 ms is the specification's figure. Below about 150 ms a fast typist
 * generates a request per character; above about 400 ms the list feels
 * like it is lagging behind the cursor.
 */
export const SEARCH_DEBOUNCE_MS = 250

export const astrologyApi = {
  searchPlaces: (query: string, signal?: AbortSignal) =>
    authed<{ places: Place[]; query: string }>(
      `/places/search?q=${encodeURIComponent(query)}`,
      { signal },
    ),

  listProfiles: () => authed<{ birth_profiles: BirthProfile[] }>('/birth-profiles'),

  getProfile: (id: string) => authed<BirthProfile>(`/birth-profiles/${id}`),

  createProfile: (input: CreateProfileInput) =>
    authed<BirthProfile>('/birth-profiles', {
      method: 'POST',
      body: JSON.stringify(input),
    }),

  // PATCH returns a profile with a DIFFERENT id from the one in the URL,
  // because correcting birth details creates a new version rather than
  // mutating the old one. Callers must use the returned id.
  updateProfile: (id: string, input: CreateProfileInput) =>
    authed<BirthProfile>(`/birth-profiles/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),

  deleteProfile: (id: string) =>
    authed<void>(`/birth-profiles/${id}`, { method: 'DELETE' }),

  versions: (id: string) =>
    authed<{ versions: BirthProfile[] }>(`/birth-profiles/${id}/versions`),

  /**
   * `type` comes from `VARGAS`, not from a literal union written here.
   *
   * It was `'D1' | 'D9'` — D10 shipped in the engine and in api-service
   * and never reached the client, so the dasamsa was uncallable from the
   * web app for a whole PR. api-service falls back to the rasi on an
   * unrecognised type rather than erroring, which is why nothing
   * complained. `varga.test.ts` now compares this list against the Go
   * constants directly.
   */
  chart: (profileId: string, type: VargaType = DEFAULT_VARGA) =>
    authed<Chart>(`/charts/${profileId}?type=${encodeURIComponent(type)}`),

  /**
   * One level at a time.
   *
   * The tree is 819 rows — 9 mahadashas, 81 antardashas, 729
   * pratyantardashas — and the timeline shows nine of them until
   * somebody drills in. Fetching all three levels to render the top one
   * is 90x the payload for the first paint of the screen users open
   * most.
   */
  dashas: (profileId: string, level: 1 | 2 | 3 = 1) =>
    authed<{ level: number; periods: DashaPeriod[] }>(
      `/charts/${profileId}/dashas?level=${level}`,
    ),

  /**
   * The question the dasha screen actually asks: where am I now.
   *
   * Answered by the server rather than by searching the fetched periods
   * in the browser, because the browser's clock is the user's and can be
   * wrong by years. A "you are here" marker derived from a wrong clock
   * is a confident wrong answer to the thing people come to this screen
   * for.
   */
  currentDashas: (profileId: string) =>
    authed<CurrentDashas>(`/charts/${profileId}/dashas/current`),

  /** Positions relative to this profile's natal Moon, plus Sade Sati. */
  transits: (profileId: string) =>
    authed<NatalTransits>(`/astrology/transits/${profileId}`),

  /**
   * Asks for a PDF. Returns a job id to poll; the document is not ready.
   *
   * 202, not 200 — the render happens on a worker, takes seconds and
   * holds a browser while it runs. Doing it inside the request would tie
   * up a request slot per download and time out under any load.
   */
  requestPdf: (profileId: string, locale: string) =>
    authed<{ job_id: string }>(
      `/charts/${profileId}/pdf?locale=${encodeURIComponent(locale)}`,
      { method: 'POST' },
    ),

  /**
   * Polls one render.
   *
   * The job id is scoped to the caller server-side — the status key is
   * composed from the authenticated user — so somebody else's job id
   * answers 404 rather than handing over a signed link.
   */
  pdfStatus: (profileId: string, jobId: string) =>
    authed<PdfStatus>(`/charts/${profileId}/pdf/${jobId}`),

  /**
   * Creates a share link.
   *
   * The response carries the only copy of the token that will ever
   * exist — listing shares afterwards deliberately does not return it.
   * So a caller that drops this response has lost the link, and the
   * remedy is to make a new one.
   */
  createShare: (profileId: string, expiresInDays?: number) =>
    authed<ShareLink>(`/charts/${profileId}/shares`, {
      method: 'POST',
      body: JSON.stringify({ expires_in_days: expiresInDays ?? 0 }),
    }),

  listShares: (profileId: string) =>
    authed<{ shares: ShareLink[] }>(`/charts/${profileId}/shares`),

  revokeShare: (profileId: string, shareId: string) =>
    authed<ShareLink>(`/charts/${profileId}/shares/${shareId}`, { method: 'DELETE' }),
}

export interface ShareLink {
  id: string
  birth_profile_id: string
  scope: string
  expires_at: string
  revoked_at?: string | null
  view_count: number
  /** Present ONLY in the response to createShare. Never on a listing. */
  token?: string
}

export interface PdfStatus {
  status: 'queued' | 'running' | 'done' | 'failed'
  /** Present only when done. Signed, and expires. */
  url?: string
  /** Present only when failed. Deliberately generic. */
  message?: string
  expires_at?: string
}
