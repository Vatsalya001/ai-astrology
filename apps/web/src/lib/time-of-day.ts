'use client'

import { useSyncExternalStore } from 'react'

export type TimeOfDay = 'morning' | 'afternoon' | 'evening'

/**
 * Morning, afternoon or evening — on the CLIENT's clock, safely.
 *
 * ── Why this is not just `new Date().getHours()` ──
 *
 * Reading the clock during render is the hydration mismatch the frontend
 * rules name explicitly. The server renders at one instant and the
 * client at another, and even a few seconds apart can straddle noon.
 * React then reports a mismatch and discards the server HTML for that
 * subtree, which on the dashboard means the whole greeting flickers.
 *
 * It cannot be an effect either: setting state from one triggers the
 * cascading render that `react-hooks/set-state-in-effect` flags, and the
 * greeting would still visibly change after paint.
 *
 * `useSyncExternalStore` is built for exactly this shape — a value owned
 * outside React with a separate server snapshot. It is what
 * `i18n/context.tsx` and `profile-context.tsx` both use, for the same
 * reason. The server renders the neutral greeting, the client corrects
 * it during hydration, and there is one render either way.
 *
 * ── Why `subscribe` does nothing ──
 *
 * The greeting does not need to change while somebody is looking at the
 * page. A timer to flip it at the stroke of noon would fire on a
 * dashboard nobody is watching, and the one reader in a thousand who has
 * the tab open across the boundary can refresh. A no-op subscribe is
 * honest about that; a timer would be complexity in service of nothing.
 */

function subscribe(): () => void {
  return () => {}
}

/**
 * Returns a string, not a Date. `useSyncExternalStore` compares
 * snapshots with `Object.is`, so returning a fresh object every call
 * would loop forever; a string is stable for the whole hour.
 */
function getSnapshot(): TimeOfDay {
  const hour = new Date().getHours()
  if (hour < 12) return 'morning'
  if (hour < 17) return 'afternoon'
  return 'evening'
}

/**
 * What the server renders.
 *
 * `null`, so the greeting has a neutral form rather than guessing. The
 * server cannot know the reader's clock — it is in their browser — and
 * "Good morning" rendered to somebody at midnight is worse than a
 * greeting with no time in it at all.
 */
function getServerSnapshot(): TimeOfDay | null {
  return null
}

export function useTimeOfDay(): TimeOfDay | null {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
}
