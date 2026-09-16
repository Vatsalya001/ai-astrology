import { authApi, AuthError } from './auth-api'

/**
 * Authenticated calls, and the access token they need.
 *
 * The token is held in a module variable — in memory, deliberately.
 * localStorage and sessionStorage are readable by any script on the
 * page, so an XSS bug there is a stolen token; a memory variable dies
 * with the tab. The refresh token is an httpOnly cookie the browser
 * sends automatically and JavaScript cannot read at all, which is what
 * makes a page reload survivable without persisting anything.
 *
 * The cost is that the token is gone on reload and has to be re-minted
 * from the cookie. That is one extra request per page load, which is the
 * right trade.
 */

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:4000'

let accessToken: string | null = null

export function setAccessToken(token: string | null): void {
  accessToken = token
}

export function getAccessToken(): string | null {
  return accessToken
}

/**
 * Mints an access token from the refresh cookie.
 *
 * Callers share one in-flight promise: on a page load several components
 * may ask at once, and without this each would rotate the refresh token.
 * Since rotation invalidates the previous one, the second would present
 * a spent token — and the server would correctly read that as REUSE and
 * revoke the whole family, logging the user out for doing nothing wrong.
 */
let refreshInFlight: Promise<string | null> | null = null

export async function ensureAccessToken(): Promise<string | null> {
  if (accessToken) return accessToken
  if (refreshInFlight) return refreshInFlight

  refreshInFlight = authApi
    .refresh()
    .then((res) => {
      accessToken = res.access_token
      return accessToken
    })
    .catch(() => {
      accessToken = null
      return null
    })
    .finally(() => {
      refreshInFlight = null
    })

  return refreshInFlight
}

/**
 * One authenticated fetch, shared by every feature client.
 *
 * Exported so `astrology-api.ts` uses the same token handling, the same
 * single 401 retry and the same error shape. A second copy would drift,
 * and the way it would drift is by forgetting the retry — which shows up
 * as users being signed out fifteen minutes into a session.
 */
export async function authed<T>(
  path: string,
  init: RequestInit & { retry?: boolean } = {},
): Promise<T> {
  const token = await ensureAccessToken()
  if (!token) {
    throw new AuthError('UNAUTHORIZED', 401, 'Not signed in.')
  }

  const res = await fetch(`${API_URL}/api/v1${path}`, {
    ...init,
    credentials: 'include',
    headers: {
      ...init.headers,
      Accept: 'application/json',
      Authorization: `Bearer ${token}`,
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
    },
  })

  // A 401 on an authenticated call usually means the 15-minute access
  // token expired mid-session. Refresh once and retry — but only once,
  // or a genuinely revoked session becomes an infinite loop.
  if (res.status === 401 && !init.retry) {
    accessToken = null
    const renewed = await ensureAccessToken()
    if (renewed) {
      return authed<T>(path, { ...init, retry: true })
    }
  }

  if (res.status === 204) return undefined as T

  const payload = (await res.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string }
  }

  if (!res.ok) {
    throw new AuthError(
      payload.error?.code ?? 'UNKNOWN',
      res.status,
      payload.error?.message ?? 'Something went wrong.',
      res.headers.get('Retry-After') ? Number(res.headers.get('Retry-After')) : undefined,
    )
  }

  return payload as T
}

export interface Profile {
  id: string
  email: string | null
  email_verified: boolean
  phone: string | null
  phone_verified: boolean
  name: string | null
  gender: string | null
  role: string
  created_at: string
}

export interface Preferences {
  preferred_language: string
  astrology_system: string
  chart_style: string
  theme: string
}

export interface DeviceSession {
  id: string
  user_agent: string
  created_at: string
  expires_at: string
  current: boolean
}

export const usersApi = {
  me: () => authed<Profile>('/users/me'),

  updateProfile: (patch: { name?: string; gender?: string }) =>
    authed<Profile>('/users/me', { method: 'PATCH', body: JSON.stringify(patch) }),

  preferences: () => authed<Preferences>('/users/me/preferences'),

  updatePreferences: (patch: Partial<Preferences>) =>
    authed<Preferences>('/users/me/preferences', {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  sessions: () => authed<{ sessions: DeviceSession[] }>('/users/me/sessions'),

  revokeSession: (id: string) =>
    authed<void>(`/users/me/sessions/${id}`, { method: 'DELETE' }),

  /** Sends a fresh code to the account's own verified contact. */
  challenge: () => authed<{ sent: boolean }>('/users/me/challenge', { method: 'POST' }),

  requestDeletion: (code: string) =>
    authed<{ deletion_scheduled_at: string }>('/users/me/delete', {
      method: 'POST',
      body: JSON.stringify({ code, confirm: 'DELETE' }),
    }),

  cancelDeletion: () => authed<void>('/users/me/delete/cancel', { method: 'POST' }),

  /**
   * Downloads the export.
   *
   * A direct navigation rather than a fetch: the response carries
   * Content-Disposition, so the browser saves it as a file. Fetching it
   * into memory and re-creating a Blob would work and would also hold
   * the user's entire history in a JavaScript variable for no reason.
   */
  exportUrl: (code: string) =>
    `${API_URL}/api/v1/users/me/export?code=${encodeURIComponent(code)}`,

  signOut: async () => {
    // Clear locally first. Even if the server call fails, this tab must
    // stop behaving as though it is signed in.
    accessToken = null
    await authApi.logout().catch(() => undefined)
  },
}
