'use client'

import { useCallback } from 'react'
import { useRouter } from 'next/navigation'

/**
 * Sends an unauthenticated visitor to sign in.
 *
 * `replace`, not `push`: Back must not return to a page that will only
 * bounce them again.
 *
 * This is a client-side convenience, not a security boundary. Every
 * endpoint behind these screens enforces authentication itself — a
 * redirect in a browser protects nothing, and treating it as protection
 * is how an unguarded API ships.
 */
export function useRequireAuth(): () => void {
  const router = useRouter()
  return useCallback(() => {
    router.replace('/auth')
  }, [router])
}
