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
  /** Tab-stop number. Null when focus was moved by a jump button. */
  n: number | null
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

  // A table is named by its <caption>.
  if (el instanceof HTMLTableElement) {
    return el.querySelector('caption')?.textContent?.trim() ?? ''
  }

  /*
    Containers are NOT named by their contents.

    This was the tool's worst bug, and it produced a false negative on
    correct markup — which is the only kind of bug an accessibility
    checker must never have.

    The chart's hidden table was reported as
    "Planetary positions — …as a table.PlanetSignHouseDegreeNakshatra
    NotesAscendantVirgo1st2", i.e. the caption with every cell in the
    first two rows run together and then cut off at 120 characters —
    before the Moon. A tester asked "which sign is your Moon in?" read
    that, could not answer, and would reasonably have concluded the table
    was unreadable. It is not: it has a caption, six `scope="col"`
    headers and a `<th>` per row, and a screen reader on that markup says
    "Moon, House, 4th".

    No screen reader announces a container's flattened text as its name.
    It announces the name — usually nothing — and then lets the user
    navigate INTO it. Falling back to textContent is right for a button
    or a link, whose content IS their name, and wrong for everything
    that holds other things.
  */
  const CONTAINERS =
    'table, ul, ol, dl, nav, main, section, article, aside, header, footer, form, div, tbody, thead, tr'
  if (el.matches(CONTAINERS)) return ''

  return el.textContent?.trim().replace(/\s+/g, ' ').slice(0, 120) ?? ''
}

/**
 * A table read the way a screen reader reads one: cell by cell, with
 * each value preceded by its column header.
 *
 * The panel exists to answer questions like "which sign is my Moon in,
 * and which house?" — and on this product that answer lives in a table.
 * Reporting only the table's NAME leaves the tester with a caption and
 * no data, which is exactly as useless as it sounds.
 *
 * This is not a full implementation of table navigation. It is the one
 * thing a tester actually does: walk the rows and check the facts are
 * there, in an order that makes sense, each one attached to its header.
 */
function readTable(table: HTMLTableElement): string[] {
  const headers = [...table.querySelectorAll('thead th')].map(
    (h) => h.textContent?.trim() ?? '',
  )

  return [...table.querySelectorAll('tbody tr')].map((row) =>
    [...row.children]
      .map((cell, index) => {
        const value = cell.textContent?.trim() ?? ''
        if (!value) return null
        // The row header names the row; it is not "Planet: Moon".
        if (cell.tagName === 'TH') return value
        const header = headers[index]
        return header ? `${header}: ${value}` : value
      })
      .filter(Boolean)
      .join(', '),
  )
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
  const [tableRows, setTableRows] = useState<string[] | null>(null)
  const counter = useRef(0)

  /*
    Set by a jump button immediately before it moves focus.

    Without it the log counted a jump as a Tab stop, and "how many tabs
    to reach the Moon" — the one number this tool exists to report — was
    inflated by every click the tester made. One session logged
    nineteen stops on a page with eight.
  */
  const jumped = useRef(false)

  // ─── focus tracking ───────────────────────────────────────────────

  useEffect(() => {
    const onFocus = (event: FocusEvent) => {
      const el = event.target as Element | null
      if (!el || el === document.body) return
      // The panel's own controls are not part of the page under test.
      if (el.closest('[data-a11y-inspector]')) return

      const viaJump = jumped.current
      jumped.current = false
      if (!viaJump) counter.current += 1

      const stop: Stop = {
        n: viaJump ? null : counter.current,
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

  /** The latest Tab-stop number, ignoring jumps. See the heading below. */
  const tabStops = stops.find((s) => s.n !== null)?.n ?? 0

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
    jumped.current = true
    el.focus()

    // Landing on a table is not the same as being able to read one.
    setTableRows(el instanceof HTMLTableElement ? readTable(el) : null)
    return true
  }, [])

  const reset = useCallback(() => {
    counter.current = 0
    jumped.current = false
    setStops([])
    setAnnouncements([])
    setCurrent(null)
    setTableRows(null)
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
              {current.n === null ? 'jumped here' : `tab stop #${current.n}`} ·{' '}
              {current.role} · &lt;{current.tag}&gt;
            </div>
          </div>
        ) : (
          <div className="mt-1 text-ink-muted">
            Nothing yet — press a jump button, then Tab.
          </div>
        )}
      </div>

      {/* ── the table, read as a screen reader would ── */}
      {tableRows && (
        <div>
          <div className="mb-1 text-xs uppercase tracking-wide text-ink-muted">
            The data table, row by row ({tableRows.length})
          </div>
          {/*
            Showing the table's NAME and nothing else was the tool's
            central failure. A tester asked "which sign is my Moon in,
            and which house?" got a caption and a truncated run of cell
            text, could not answer, and had every reason to blame the
            product — whose table is in fact captioned, has six
            `scope="col"` headers and a `<th>` per row.

            Each value is prefixed with its column header here, which is
            what a screen reader does when moving between cells and what
            makes a row answer a question on its own.
          */}
          <ol className="space-y-1">
            {tableRows.map((row, i) => (
              <li key={i} className="rounded bg-elevated/60 px-2 py-1 text-xs text-ink">
                {row}
              </li>
            ))}
          </ol>
        </div>
      )}

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
          {/*
              Derived from state, not from `counter.current`.

              Reading a ref during render is both a lint error and a real
              one: refs do not trigger a re-render, so the heading would
              show whatever the count was when something ELSE last caused
              one. `stops` is newest-first, so the first numbered entry
              carries the latest tab-stop number — correct even once the
              log has been capped at forty.
          */}
          Focus order — {tabStops} tab stop{tabStops === 1 ? '' : 's'}
        </div>
        <ol className="space-y-0.5">
          {stops.map((s, i) => (
            <li key={`${s.n ?? 'jump'}-${i}`} className="text-xs">
              {/* A jump is shown but not numbered: it is not a Tab press,
                  and counting it makes "how many tabs to reach X" wrong. */}
              <span className="text-ink-faint">{s.n === null ? '→' : `${s.n}.`}</span>{' '}
              <span className={s.name ? 'text-ink' : 'text-ink-muted italic'}>
                {s.name || '(no name — normal for a container)'}
              </span>{' '}
              <span className="text-ink-faint">({s.role})</span>
            </li>
          ))}
        </ol>
      </div>
    </aside>
  )
}
