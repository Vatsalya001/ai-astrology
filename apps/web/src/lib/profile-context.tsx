'use client'

import { createContext, useCallback, useContext, useSyncExternalStore } from 'react'

/**
 * Which birth profile the Kundli screens are showing.
 *
 * Until this existed every screen did `profiles[0]`, which is a profile
 * switcher that always switches to the same person. A user with charts
 * for themselves, a partner and a child could see only the first.
 *
 * ── Why a store and not a URL parameter ──
 *
 * A profile id in the URL would put a database identifier in something
 * users copy, paste into messages and hand to support. It is not PII by
 * itself, but it is a durable handle to a record that IS, and the
 * security rules are explicit that birth details never go in a query
 * string. The selection is a view preference; it lives where the locale
 * lives.
 *
 * ── Why useSyncExternalStore ──
 *
 * The same reason as the locale: localStorage does not exist on the
 * server, so reading it during render produces different HTML on each
 * side and a hydration mismatch, and reading it in an effect triggers a
 * cascading render that React's lint rule flags. The server snapshot is
 * null — no selection — and the client corrects it during hydration.
 */

const STORAGE_KEY = 'ayana.birthProfileId'

// Same-tab subscribers. The `storage` event only fires in OTHER tabs, so
// without this a switch would not re-render the tab that made it — the
// one place it obviously must.
const listeners = new Set<() => void>()

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange)
  window.addEventListener('storage', onChange)
  return () => {
    listeners.delete(onChange)
    window.removeEventListener('storage', onChange)
  }
}

function getSnapshot(): string | null {
  return window.localStorage.getItem(STORAGE_KEY)
}

/** What the server renders: no selection, so screens fall back to the first. */
function getServerSnapshot(): string | null {
  return null
}

interface ProfileContextValue {
  /**
   * The stored id, which may name a profile that no longer exists.
   *
   * Deliberately NOT validated here — this store has no idea what
   * profiles exist, and an id is only meaningful against a fetched list.
   * Screens resolve it with `resolveProfile`, which falls back rather
   * than requesting a deleted profile forever.
   */
  selectedId: string | null
  select: (id: string) => void
}

const ProfileContext = createContext<ProfileContextValue | null>(null)

export function ProfileProvider({ children }: { children: React.ReactNode }) {
  const selectedId = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)

  const select = useCallback((id: string) => {
    window.localStorage.setItem(STORAGE_KEY, id)
    for (const listener of listeners) listener()
  }, [])

  return (
    <ProfileContext.Provider value={{ selectedId, select }}>{children}</ProfileContext.Provider>
  )
}

export function useSelectedProfile(): ProfileContextValue {
  const ctx = useContext(ProfileContext)
  if (!ctx) throw new Error('useSelectedProfile must be used inside a ProfileProvider')
  return ctx
}

/**
 * Resolve a stored id against the profiles that actually exist.
 *
 * The stored id outlives the profile. Deleting a birth profile, or
 * editing one — which creates a NEW version with a new id rather than
 * mutating the old one, per Phase 2 — leaves localStorage pointing at a
 * record that is gone. Requesting it gives a 404, and the ownership
 * middleware returns 404 for another user's profile too, so the screen
 * cannot tell "deleted" from "not yours" and would show an error state
 * forever, on every visit, with the fix invisible.
 *
 * Falling back to the first profile is recoverable and obvious: the
 * switcher shows which one is selected, and one tap changes it.
 */
export function resolveProfile<T extends { id: string }>(
  profiles: T[],
  selectedId: string | null,
): T | null {
  if (profiles.length === 0) return null
  return profiles.find((p) => p.id === selectedId) ?? profiles[0]!
}
