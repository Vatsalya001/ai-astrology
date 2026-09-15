'use client'

import { useCallback } from 'react'
import { useRouter } from 'next/navigation'

import { AuthError } from './auth-api'

/**
 * Sends an unauthenticated visitor to sign in — and ONLY an
 * unauthenticated one.
 *
 * The previous version took no argument and redirected on every failure,
 * which made a dropped connection indistinguishable from a dead session.
 * A user on a flaky mobile connection, or anyone loading a screen during
 * a deploy, was silently thrown out to /auth: they lose their place, are
 * told nothing, and if the outage persists they cannot sign in either —
 * so the reasonable conclusion is that their account is gone. That is the
 * failure mode this product will hit most often, given where its users
 * are.
 *
 * So the caller now gets an answer rather than a side effect:
 *
 *   handled === true   the session really is gone; a redirect is under way
 *   handled === false  something else broke; SHOW AN ERROR, do not redirect
 *
 * `replace`, not `push`: Back must not return to a page that will only
 * bounce them again.
 *
 * This is a client-side convenience, not a security boundary. Every
 * endpoint behind these screens enforces authentication itself — a
 * redirect in a browser protects nothing, and treating it as protection
 * is how an unguarded API ships.
 */
export function useRequireAuth(): (err: unknown) => boolean {
  const router = useRouter()

  return useCallback(
    (err: unknown) => {
      if (!isAuthFailure(err)) return false
      router.replace('/auth')
      return true
    },
    [router],
  )
}

/**
 * A genuine "you are not signed in", as opposed to anything else.
 *
 * Status 401 and the UNAUTHORIZED code are both checked because they come
 * from different places: the server sends the status, and `users-api.ts`
 * raises the code locally when there is no token to send at all.
 *
 * Deliberately NOT included: `NETWORK` (status 0, fetch itself failed),
 * 5xx, and 429. None of those say anything about the session.
 */
function isAuthFailure(err: unknown): boolean {
  return err instanceof AuthError && (err.status === 401 || err.code === 'UNAUTHORIZED')
}
