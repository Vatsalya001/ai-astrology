'use client'

import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * A screen reader you can read instead of hear.
 *
 * ── Why this exists ──
 *
 * The Phase 3 gate asks for a manual keyboard and screen-reader pass.
 * Doing that with Orca means installing it, learning its modifier keys,
 * working out why it is silent on Wayland, and then holding a page's
 * worth of speech in your head — before you have learned anything about
 * the product. Most of what the pass actually asks is not a judgement at
 * all: "does this control have a name", "what is the focus order",
 * "was that announced". Those are readable facts.
 *
 * So this panel shows, in the page:
 *
 *   - what the focused element WOULD be announced as, computed the same
 *     way a screen reader computes it
 *   - every focus stop in order, numbered, so "how many tabs to reach
 *     the Moon" is a number rather than an impression
 *   - live-region announcements as they happen, which are otherwise
 *     invisible: an error that renders but is never announced looks
 *     identical to one that is
 *
 * It does NOT replace a real screen reader for the last question —
 * whether the result is *comprehensible* by ear. Nothing on a screen can
 * answer that. It replaces the other twenty steps.
 *
 * ── Why it is not in the production bundle ──
 *
 * Loaded only when the URL carries `?a11y=1`, through a dynamic import,
 * so it lands in its own chunk and never counts against the first-load
 * budget that `bundle-budget.json` enforces. A developer tool that costs
 * every user 12 KB is a developer tool that gets deleted.
 */

interface Stop {
  n: number
  role: string
  name: string
  tag: string
}

interface Announcement {
  at: string
  politeness: string
  text: string
}

/**
 * The accessible name, by the same precedence a screen reader uses.
 *
 * aria-label wins, then aria-labelledby, then the element's own text.
 * Not a full implementation of the accname spec — that is hundreds of
 * lines — but it covers every case this product actually produces, and
 * where it is wrong it is wrong in the safe direction: it reports LESS
 * than a screen reader would, so a control that looks unlabelled here is
 * worth checking rather than dismissed.
 */
function accessibleName(el: Element): string {
  const label = el.getAttribute('aria-label')
  if (label?.trim()) return label.trim()

  const labelledBy = el.getAttribute('aria-labelledby')
  if (labelledBy) {
    const text = labelledBy
      .split(/\s+/)
      .map((id) => document.getElementById(id)?.textContent?.trim() ?? '')
      .filter(Boolean)
      .join(' ')
    if (text) return text
  }

  // A form control's <label>.
  if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement) {
    const id = el.getAttribute('id')
    if (id) {
      const lbl = document.querySelector(`label[for="${CSS.escape(id)}"]`)
      if (lbl?.textContent?.trim()) return lbl.textContent.trim()
    }
  }

  return el.textContent?.trim().replace(/\s+/g, ' ').slice(0, 120) ?? ''
}

/** The role a screen reader would announce, explicit or implied. */
function role(el: Element): string {
  const explicit = el.getAttribute('role')
  if (explicit) return explicit

  const implied: Record<string, string> = {
    A: 'link',
    BUTTON: 'button',
    INPUT: 'input',
    SELECT: 'combobox',
    TEXTAREA: 'textbox',
    TABLE: 'table',
    TR: 'row',
    TH: 'columnheader',
    TD: 'cell',
    UL: 'list',
    OL: 'list',
    LI: 'listitem',
    MAIN: 'main',
    NAV: 'navigation',
    H1: 'heading 1',
    H2: 'heading 2',
    H3: 'heading 3',
  }
  return implied[el.tagName] ?? el.tagName.toLowerCase()
}

export function A11yInspector() {
  const [stops, setStops] = useState<Stop[]>([])
  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [current, setCurrent] = useState<Stop | null>(null)
  const [open, setOpen] = useState(true)
  const counter = useRef(0)

  // ─── focus tracking ───────────────────────────────────────────────

  useEffect(() => {
    const onFocus = (event: FocusEvent) => {
      const el = event.target as Element | null
      if (!el || el === document.body) return
      // The panel's own controls are not part of the page under test.
      if (el.closest('[data-a11y-inspector]')) return

      counter.current += 1
      const stop: Stop = {
        n: counter.current,
        role: role(el),
        name: accessibleName(el),
        tag: el.tagName.toLowerCase(),
      }
      setCurrent(stop)
      setStops((prev) => [stop, ...prev].slice(0, 40))
    }

    document.addEventListener('focusin', onFocus, true)
    return () => document.removeEventListener('focusin', onFocus, true)
  }, [])

  // ─── live regions ─────────────────────────────────────────────────

  /*
    Announcements are invisible without this.

    A `role="status"` that renders but is never announced looks exactly
    like one that is — on screen they are the same pixels. The only way
    to tell is to watch the DOM mutate, which is what a screen reader
    does and what this does.
  */
  useEffect(() => {
    const observer = new MutationObserver((mutations) => {
      for (const m of mutations) {
        const host = (m.target as Element).closest?.(
          '[role="status"], [role="alert"], [aria-live]',
        )
        if (!host || host.closest('[data-a11y-inspector]')) continue

        const text = host.textContent?.trim().replace(/\s+/g, ' ') ?? ''
        if (!text) continue

        const politeness =
          host.getAttribute('aria-live') ??
          (host.getAttribute('role') === 'alert' ? 'assertive' : 'polite')

        setAnnouncements((prev) => {
          // The same text firing twice in a row is one announcement; a
          // re-render that changes nothing is not something a screen
          // reader would read out again.
          if (prev[0]?.text === text) return prev
          return [
            { at: new Date().toLocaleTimeString(), politeness, text },
            ...prev,
          ].slice(0, 15)
        })
      }
    })

    observer.observe(document.body, {
      subtree: true,
      childList: true,
      characterData: true,
    })
    return () => observer.disconnect()
  }, [])

  // ─── the jump buttons ─────────────────────────────────────────────

  /*
    Buttons, because "click on empty dark space to put focus in the page"
    is an instruction nobody should have to follow. Each of these does
    the thing a tester actually wants and then hands focus to the page,
    so the next Tab starts where it should.
  */
  const focusFirst = useCallback((selector: string) => {
    const el = document.querySelector<HTMLElement>(selector)
    if (!el) return false
    if (!el.hasAttribute('tabindex') && !el.matches('a,button,input,select,textarea')) {
      el.setAttribute('tabindex', '-1')
    }
    el.focus()
    return true
  }, [])

  const reset = useCallback(() => {
    counter.current = 0
    setStops([])
    setAnnouncements([])
    setCurrent(null)
  }, [])

  if (!open) {
    return (
      <button
        type="button"
        data-a11y-inspector
        onClick={() => setOpen(true)}
        className="fixed bottom-4 right-4 z-[9999] rounded-full bg-accent px-4 py-2 text-sm font-medium text-white shadow-lg"
      >
        a11y
      </button>
    )
  }

  return (
    <aside
      data-a11y-inspector
      /*
        NOT aria-hidden.

        That was the first instinct — keep the panel out of the tree it
        is inspecting — and it was the wrong mechanism twice over. It
        does not remove anything from the TAB order, so a keyboard user
        would still land on buttons that announce nothing; and it makes
        the tool unusable by the people most likely to need it. A
        screen-reader user debugging their own product deserves a
        debugger they can read.

        Keeping the panel out of the focus LOG is what was actually
        wanted, and the `closest('[data-a11y-inspector]')` guard in the
        focus handler already does exactly that.
      */
      className="fixed bottom-0 right-0 z-[9999] flex max-h-[85vh] w-[420px] flex-col
                 gap-3 overflow-auto rounded-tl-lg border-l border-t border-grid
                 bg-[#0F1530] p-4 text-sm shadow-2xl"
    >
      <div className="flex items-center justify-between">
        <strong className="text-ink">Accessibility inspector</strong>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={reset}
            className="rounded border border-grid px-2 py-1 text-xs text-ink-muted hover:text-ink"
          >
            Reset
          </button>
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="rounded border border-grid px-2 py-1 text-xs text-ink-muted hover:text-ink"
          >
            Hide
          </button>
        </div>
      </div>

      {/* ── jump buttons ── */}
      <div className="flex flex-wrap gap-2">
        {(
          [
            ['Start of page', 'a[href="#main"], main'],
            ['Main content', 'main'],
            ['The chart', 'svg[role="group"]'],
            ['The data table', 'table'],
          ] as ReadonlyArray<readonly [string, string]>
        ).map(([label, selector]) => (
          <button
            key={label}
            type="button"
            onClick={() => focusFirst(selector)}
            className="rounded bg-accent/25 px-2.5 py-1.5 text-xs text-ink hover:bg-accent/40"
          >
            {label}
          </button>
        ))}
      </div>

      <p className="text-xs leading-relaxed text-ink-muted">
        Press <kbd className="rounded bg-elevated px-1">Tab</kbd> to walk the page. Each
        stop is logged below with the name a screen reader would announce.
      </p>

      {/* ── what is focused now ── */}
      <div className="rounded border border-grid bg-elevated/60 p-3">
        <div className="text-xs uppercase tracking-wide text-ink-muted">Focused now</div>
        {current ? (
          <div className="mt-1">
            <div className="text-base text-ink">
              {current.name || (
                <span className="text-danger">⚠ no accessible name</span>
              )}
            </div>
            <div className="mt-0.5 text-xs text-ink-muted">
              stop #{current.n} · {current.role} · &lt;{current.tag}&gt;
            </div>
          </div>
        ) : (
          <div className="mt-1 text-ink-muted">
            Nothing yet — press a jump button, then Tab.
          </div>
        )}
      </div>

      {/* ── announcements ── */}
      <div>
        <div className="mb-1 text-xs uppercase tracking-wide text-ink-muted">
          Announced ({announcements.length})
        </div>
        {announcements.length === 0 ? (
          <div className="text-xs text-ink-faint">
            Nothing announced yet. Errors and status messages appear here.
          </div>
        ) : (
          <ul className="space-y-1">
            {announcements.map((a, i) => (
              <li key={i} className="rounded bg-elevated/60 px-2 py-1 text-xs">
                <span className="text-gold">{a.politeness}</span>{' '}
                <span className="text-ink">{a.text}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* ── focus order ── */}
      <div>
        <div className="mb-1 text-xs uppercase tracking-wide text-ink-muted">
          Focus order ({stops.length})
        </div>
        <ol className="space-y-0.5">
          {stops.map((s) => (
            <li key={s.n} className="text-xs">
              <span className="text-ink-faint">{s.n}.</span>{' '}
              <span className={s.name ? 'text-ink' : 'text-danger'}>
                {s.name || '⚠ unnamed'}
              </span>{' '}
              <span className="text-ink-faint">({s.role})</span>
            </li>
          ))}
        </ol>
      </div>
    </aside>
  )
}
