'use client'

import { createContext, useCallback, useContext, useSyncExternalStore } from 'react'

import {
  getDictionary,
  interpolate,
  type Dictionary,
  type Locale,
} from './dictionaries'

/**
 * The active locale, and the copy for it.
 *
 * A context rather than each screen calling `getDictionary('en')`, which
 * is what PR 8a did and which silently hardcoded English everywhere. A
 * dictionary nobody can switch is a dictionary that does nothing.
 *
 * ── Why useSyncExternalStore and not useState + useEffect ──
 *
 * The locale lives in localStorage, which does not exist on the server.
 * Reading it during render produces different HTML on each side and a
 * hydration mismatch; reading it in an effect and calling setState
 * triggers a cascading render, which React's lint rule flags.
 *
 * `useSyncExternalStore` is built for exactly this shape: a value owned
 * outside React, with a separate server snapshot. The server always
 * renders English, the client corrects it during hydration, and there is
 * one render either way.
 */

const STORAGE_KEY = 'ayana.locale'

// Same-tab subscribers. The `storage` event only fires in OTHER tabs, so
// without this a language change would not re-render the tab that made
// it — the one place it obviously must.
const listeners = new Set<() => void>()

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange)
  window.addEventListener('storage', onChange)
  return () => {
    listeners.delete(onChange)
    window.removeEventListener('storage', onChange)
  }
}

function getSnapshot(): Locale {
  const stored = window.localStorage.getItem(STORAGE_KEY)
  return stored === 'hi' ? 'hi' : 'en'
}

/**
 * What the server renders.
 *
 * Always English. The server cannot know the preference — it is in the
 * browser — and guessing from Accept-Language would make the page
 * uncacheable for a first paint the client corrects anyway.
 */
function getServerSnapshot(): Locale {
  return 'en'
}

interface LocaleContextValue {
  locale: Locale
  t: Dictionary
  setLocale: (locale: Locale) => void
  /** Interpolates {placeholders} in a string from the dictionary. */
  fill: (template: string, values: Record<string, string | number>) => string
}

const LocaleContext = createContext<LocaleContextValue | null>(null)

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const locale = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)

  const setLocale = useCallback((next: Locale) => {
    // A display preference, not personal data — safe to persist, and
    // persisting it is what stops the UI flipping back to English on
    // every page load before the profile fetch returns.
    window.localStorage.setItem(STORAGE_KEY, next)
    document.documentElement.lang = next
    for (const listener of listeners) listener()
  }, [])

  return (
    <LocaleContext.Provider
      value={{ locale, t: getDictionary(locale), setLocale, fill: interpolate }}
    >
      {children}
    </LocaleContext.Provider>
  )
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext)
  if (!ctx) {
    throw new Error('useLocale must be used inside a LocaleProvider')
  }
  return ctx
}
