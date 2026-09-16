'use client'

import { useEffect, useRef, useState } from 'react'

import { Input } from '@/components/ui/input'
import {
  astrologyApi,
  MIN_QUERY_LENGTH,
  SEARCH_DEBOUNCE_MS,
  type Place,
} from '@/lib/astrology-api'
import { getDictionary } from '@/lib/i18n/dictionaries'
import { track } from '@/lib/analytics'
import { cn } from '@/lib/utils'

/**
 * Birth-place autocomplete.
 *
 * The client sends a place ID, never coordinates. Latitude, longitude
 * and IANA zone all have to agree with each other, and a browser that
 * sends its own three values will eventually send three that do not —
 * producing a chart that is wrong in a way nothing downstream can
 * detect. The server resolves the ID against its own gazetteer.
 *
 * Population ranking is the whole feature: "jaip" has to return Jaipur,
 * Rajasthan ahead of Jaipur, Odisha. A user who does not see their city
 * in the first three results types something else or gives up, and this
 * is the last step before the chart.
 */
export function PlaceSearch({
  selected,
  onSelect,
  disabled,
}: {
  selected: Place | null
  onSelect: (place: Place) => void
  disabled?: boolean
}) {
  const t = getDictionary('en')

  const [query, setQuery] = useState(selected ? describe(selected) : '')

  // Results carry the query they came from, and the render below shows
  // them only when that still matches what is typed.
  //
  // Storing a bare Place[] instead needs a setState in the effect body
  // to clear it when the query shortens — which the React compiler
  // rejects, correctly, as a cascading render. Keying the results makes
  // the stale case unrepresentable rather than cleared after the fact.
  const [outcome, setOutcome] = useState<{
    query: string
    places: Place[]
    failed: boolean
  } | null>(null)
  const [searching, setSearching] = useState(false)
  const [open, setOpen] = useState(false)
  const [highlighted, setHighlighted] = useState(-1)

  // Tracks the query the user has actually committed to searching, so
  // choosing a result does not immediately trigger a search for the
  // text that choosing it just wrote into the box.
  const suppressNextSearch = useRef(false)

  useEffect(() => {
    if (suppressNextSearch.current) {
      suppressNextSearch.current = false
      return
    }

    const trimmed = query.trim()
    // No setState here: below the threshold there is simply nothing to
    // search, and the render derives an empty list from the query.
    if (trimmed.length < MIN_QUERY_LENGTH) return

    // One AbortController per keystroke. Without it, responses can
    // overtake each other on the wire and the dropdown flickers back to
    // results for a prefix the user has already typed past — which looks
    // like the search is broken even though every request succeeded.
    const controller = new AbortController()

    const timer = setTimeout(() => {
      setSearching(true)
      astrologyApi
        .searchPlaces(trimmed, controller.signal)
        .then((response) => {
          // The abort signal, checked BEFORE anything is stored.
          //
          // Comparing response.query against `trimmed` alone does not
          // work and a test caught it: `trimmed` is captured per effect
          // run, so a stale request compares its own query against its
          // own query, matches, and overwrites a newer result. The
          // signal is the only thing that knows this run was superseded.
          if (controller.signal.aborted) return
          // Second line of defence, for a response that somehow arrives
          // for a different query on a live controller.
          if (response.query.trim() !== trimmed) return
          setOutcome({ query: trimmed, places: response.places, failed: false })
          setOpen(true)
          setHighlighted(-1)
          // The COUNT, never the query. A place search is location PII:
          // "jaip" from a phone in Rajasthan is most of a birthplace.
          track('place_search_performed', { result_count: response.places.length })
        })
        .catch((err: unknown) => {
          if (err instanceof DOMException && err.name === 'AbortError') return
          setOutcome({ query: trimmed, places: [], failed: true })
        })
        .finally(() => {
          if (!controller.signal.aborted) setSearching(false)
        })
    }, SEARCH_DEBOUNCE_MS)

    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query])

  function choose(place: Place) {
    suppressNextSearch.current = true
    setQuery(describe(place))
    setOutcome(null)
    setOpen(false)
    setHighlighted(-1)
    onSelect(place)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (!open || results.length === 0) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setHighlighted((index) => (index + 1) % results.length)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setHighlighted((index) => (index <= 0 ? results.length - 1 : index - 1))
    } else if (e.key === 'Enter' && highlighted >= 0) {
      // Only when something is highlighted. Otherwise Enter must submit
      // the form, which is what a keyboard user expects after typing.
      const chosen = results[highlighted]
      if (chosen) {
        e.preventDefault()
        choose(chosen)
      }
    } else if (e.key === 'Escape') {
      setOpen(false)
      setHighlighted(-1)
    }
  }

  const trimmed = query.trim()

  // Everything below is DERIVED from the query and the last outcome, so
  // a shortened query shows nothing without any state being cleared.
  const current = outcome && outcome.query === trimmed ? outcome : null
  const results = current?.places ?? []
  const failed = current?.failed ?? false

  const showKeepTyping = trimmed.length > 0 && trimmed.length < MIN_QUERY_LENGTH
  const showNoResults =
    open && !searching && !failed && current !== null && results.length === 0

  return (
    <div className="relative">
      <label htmlFor="place" className="mb-2 block text-sm font-medium">
        {t.birth.placeLabel}
      </label>

      <Input
        id="place"
        name="place"
        role="combobox"
        aria-expanded={open}
        aria-controls="place-results"
        aria-autocomplete="list"
        aria-activedescendant={highlighted >= 0 ? `place-option-${highlighted}` : undefined}
        autoComplete="off"
        disabled={disabled}
        placeholder={t.birth.placePlaceholder}
        value={query}
        onChange={(e) => {
          setQuery(e.target.value)
          setOpen(true)
        }}
        onKeyDown={handleKeyDown}
        className="h-12 text-base"
      />

      {/* One live region for every transient state, so a screen reader
          announces "searching", "no places found" and a result count
          without the list itself stealing focus. */}
      <p aria-live="polite" className="sr-only">
        {searching
          ? t.birth.placeSearching
          : results.length > 0
            ? `${results.length}`
            : ''}
      </p>

      {showKeepTyping && (
        <p className="mt-2 text-sm text-ink-muted">{t.birth.placeKeepTyping}</p>
      )}
      {failed && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {t.birth.placeSearchFailed}
        </p>
      )}
      {showNoResults && (
        <p className="mt-2 text-sm text-ink-muted">{t.birth.placeNoResults}</p>
      )}

      {/* Generic divs, not ul/li. The ARIA combobox pattern needs
          listbox and option roles, and putting those on list elements is
          a lint error for good reason: an <li role="option"> claims to
          be two things at once. The roles ARE the semantics here.

          The options are deliberately not tab stops. In this pattern the
          input keeps focus and drives the list with arrow keys via
          aria-activedescendant, which is what a screen reader expects;
          making each option focusable would put a dozen extra stops
          between the search box and the submit button. */}
      {open && results.length > 0 && (
        <div
          id="place-results"
          role="listbox"
          aria-label={t.birth.placeLabel}
          className="absolute z-10 mt-2 max-h-64 w-full overflow-y-auto rounded-lg border border-border bg-surface-2 py-1 shadow-lg"
        >
          {results.map((place, index) => (
            <div
              key={place.id}
              id={`place-option-${index}`}
              role="option"
              aria-selected={index === highlighted}
              // -1, not 0: focusable programmatically but NOT a tab
              // stop. The input keeps focus and moves the selection with
              // aria-activedescendant; twelve tab stops between the
              // search box and the submit button would be worse for a
              // keyboard user, not better.
              tabIndex={-1}
              onMouseEnter={() => setHighlighted(index)}
              // mouseDown, not click: click fires after blur, and the
              // blur would close the list before the selection lands.
              onMouseDown={(e) => {
                e.preventDefault()
                choose(place)
              }}
              className={cn(
                'flex cursor-pointer flex-col items-start px-4 py-2.5 text-left transition-colors',
                index === highlighted ? 'bg-gold/10' : 'hover:bg-gold/5',
              )}
            >
              <span className="text-sm">{place.name}</span>
              <span className="text-xs text-ink-muted">
                {[place.admin1, place.country_code].filter(Boolean).join(', ')}
              </span>
            </div>
          ))}
        </div>
      )}

      {selected && !open && (
        <p className="mt-2 flex items-center gap-1.5 text-sm text-ink-muted">
          {/* A glyph as well as the colour: roughly 8% of men cannot
              distinguish a green tick from grey text by hue alone. */}
          <span aria-hidden="true">✓</span>
          {t.birth.placeSelected}: {describe(selected)}
        </p>
      )}
    </div>
  )
}

function describe(place: Place): string {
  return [place.name, place.admin1, place.country_code].filter(Boolean).join(', ')
}
