/**
 * Browser-side auth calls.
 *
 * `credentials: 'include'` on every request: the refresh token is an
 * httpOnly cookie, so the browser must be told to send it. Without this
 * the cookie exists and is never presented, and refresh silently fails
 * in a way that looks like an expired session.
 */

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:4000'

export type Channel = 'email' | 'phone'

export interface AuthUser {
  id: string
  role: string
}

export interface TokenResponse {
  access_token: string
  expires_at: string
  user: AuthUser
  is_new_user: boolean
}

/**
 * A failure the UI can act on.
 *
 * `code` comes from the API's stable vocabulary; `retryAfter` is the
 * Retry-After header in seconds. The UI branches on the code, never on
 * the message — messages are copy and change.
 */
export class AuthError extends Error {
  readonly code: string
  readonly status: number
  readonly retryAfter?: number

  constructor(code: string, status: number, message: string, retryAfter?: number) {
    super(message)
    this.name = 'AuthError'
    this.code = code
    this.status = status
    this.retryAfter = retryAfter
  }
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string }
}

async function post<T>(path: string, body: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(`${API_URL}/api/v1${path}`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    // A network failure, not an API response. Distinguished so the UI
    // can say "check your connection" rather than "that code is wrong".
    //
    // The cause is deliberately dropped: it is a fetch TypeError with no
    // detail worth surfacing, and attaching it would tempt a later
    // handler into rendering it.
    throw new AuthError('NETWORK', 0, 'Could not reach the server.')
  }

  if (res.status === 204) {
    return undefined as T
  }

  const payload = (await res.json().catch(() => ({}))) as ErrorEnvelope & Record<string, unknown>

  if (!res.ok) {
    const header = res.headers.get('Retry-After')
    throw new AuthError(
      payload.error?.code ?? 'UNKNOWN',
      res.status,
      payload.error?.message ?? 'Something went wrong.',
      header ? Number(header) : undefined,
    )
  }

  return payload as T
}

export interface Providers {
  google: boolean
}

export const authApi = {
  /**
   * Which social sign-in buttons to render.
   *
   * Asked rather than read from this app's own environment, so there is
   * one source of truth. Two copies of "is Google configured" drift, and
   * the failure mode is a button that navigates to a JSON error page.
   */
  providers: async (): Promise<Providers> => {
    try {
      const res = await fetch(`${API_URL}/api/v1/auth/providers`, {
        headers: { Accept: 'application/json' },
      })
      if (!res.ok) return { google: false }
      return (await res.json()) as Providers
    } catch {
      // Fail closed. A button that cannot work is worse than no button,
      // and email sign-in on this page is unaffected.
      return { google: false }
    }
  },

  /** A top-level navigation, not a fetch — the browser must follow the
   *  redirect to Google's own origin, which CORS would never allow. */
  oauthURL: (returnTo?: string): string =>
    `${API_URL}/api/v1/auth/oauth/google` +
    (returnTo ? `?return_to=${encodeURIComponent(returnTo)}` : ''),

  requestOTP: (channel: Channel, identifier: string, locale: string) =>
    post<{ sent: boolean; expires_in_seconds: number }>('/auth/otp/request', {
      channel,
      identifier,
      locale,
    }),

  verifyOTP: (channel: Channel, identifier: string, code: string) =>
    post<TokenResponse>('/auth/otp/verify', { channel, identifier, code }),

  refresh: () => post<TokenResponse>('/auth/refresh', {}),

  logout: () => post<void>('/auth/logout', {}),
}
