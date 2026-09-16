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
}
