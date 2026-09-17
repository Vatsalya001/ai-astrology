'use client'

import { useState } from 'react'

import { AstroTerm } from '@/components/AstroTerm'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { dignity, formatDegree, formatNakshatra } from './format'
import { RETROGRADE_MARK, ordinal, planetAbbreviation, planetLabel } from './glyphs'
import type { PlanetPlacement } from './types'

/**
 * The planetary positions table.
 *
 * ── One table, restacked. Not two DOMs. ──
 *
 * The spec's rule is "dense on desktop, card-stacked on mobile; never a
 * horizontally-scrolling table on a phone, it reads as broken". The
 * tempting implementation renders a `<table>` for wide screens and a
 * list of cards for narrow ones, with `hidden md:block` on one and
 * `md:hidden` on the other. That ships both to every device, doubles the
 * DOM, and — because `hidden` is a CSS concern and the a11y tree follows
 * `display: none` only when the browser agrees — has a real chance of
 * reading the whole table twice to a screen reader.
 *
 * So there is one `<table>`, and CSS restacks each row into a card below
 * `md`. Every cell carries its column name in `data-label`, rendered as
 * a `::before` at mobile, because a bare "Leo" in a stack of values with
 * no headers is unreadable.
 *
 * ── Why the roles are written out ──
 *
 * Changing `display` on a `<tr>` or `<td>` has historically dropped the
 * element from the table mapping in the accessibility tree, which is the
 * failure the restack invites: a table that reads as a pile of text.
 *
 * Measured in the Chromium this project tests against, it does NOT
 * happen — the computed tree is table → row → cell either way, with or
 * without these roles. That was checked rather than assumed, after this
 * comment first asserted the stripping as current fact. It is not, here.
 *
 * The roles stay because the cost is three attributes and the failure is
 * silent and total, and because "here" is one engine at one version and
 * the app ships to WebKit too. But the justification is defence against
 * an engine difference this suite cannot observe, not a bug it can — and
 * `kundli-planets.spec.ts` asserts the computed tree directly so a
 * regression from ANY cause fails, rather than trusting either claim.
 */
export function PlanetTable({
  planets,
  onSelect,
  className,
}: {
  planets: PlanetPlacement[]
  /** Phase 5 passes a handler here; without one the sheet is internal. */
  onSelect?: (planet: PlanetPlacement) => void
  className?: string
}) {
  const { t } = useLocale()
  const [open, setOpen] = useState<PlanetPlacement | null>(null)

  if (planets.length === 0) {
    return (
      <p className="rounded-lg border border-border p-6 text-sm text-ink-muted">
        {t.chart.planetsEmpty}
      </p>
    )
  }

  function select(planet: PlanetPlacement) {
    onSelect?.(planet)
    setOpen(planet)
  }

  return (
    <>
      <table role="table" className={cn('w-full text-sm', className)}>
        <caption className="sr-only">{t.chart.planetsCaption}</caption>

        {/*
          Headers are hidden at mobile, not removed: the data-label
          pseudo-elements take over visually, but the header cells must
          stay in the DOM or the a11y tree loses the column associations
          that make the restacked cells make sense.
        */}
        <thead className="max-md:sr-only">
          <tr role="row" className="border-b border-border text-left text-ink-muted">
            <th role="columnheader" scope="col" className="py-2 pr-3 font-medium">
              {t.chart.colPlanet}
            </th>
            <th role="columnheader" scope="col" className="py-2 pr-3 font-medium">
              {t.chart.colSign}
            </th>
            <th role="columnheader" scope="col" className="py-2 pr-3 font-medium">
              {t.chart.colDegree}
            </th>
            <th role="columnheader" scope="col" className="py-2 pr-3 font-medium">
              {t.chart.colHouse}
            </th>
            <th role="columnheader" scope="col" className="py-2 pr-3 font-medium">
              {t.chart.colNakshatra}
            </th>
            <th role="columnheader" scope="col" className="py-2 font-medium">
              {t.chart.colStatus}
            </th>
          </tr>
        </thead>

        <tbody>
          {planets.map((planet) => (
            <tr
              role="row"
              key={planet.planet}
              className={cn(
                'border-b border-border/60 align-top',
                // The card stack. Each cell becomes a labelled line.
                'max-md:mb-3 max-md:block max-md:rounded-lg max-md:border max-md:p-3',
              )}
            >
              <Cell label={t.chart.colPlanet} className="py-2 pr-3 font-medium text-ink">
                <button
                  type="button"
                  onClick={() => select(planet)}
                  className={cn(
                    'text-left underline decoration-dotted decoration-ink-muted underline-offset-4',
                    'hover:decoration-gold focus-visible:outline-none focus-visible:ring-2',
                    'focus-visible:ring-ring focus-visible:ring-offset-2',
                    'focus-visible:ring-offset-background rounded-sm',
                  )}
                  aria-label={planetLabel(planet)}
                >
                  <span aria-hidden="true" className="mr-1.5 text-ink-muted">
                    {planetAbbreviation(planet.planet)}
                  </span>
                  {planet.planet}
                </button>
              </Cell>

              <Cell label={t.chart.colSign} className="py-2 pr-3">
                {planet.sign}
              </Cell>

              <Cell label={t.chart.colDegree} className="py-2 pr-3 tabular-nums">
                {formatDegree(planet.degree)}
              </Cell>

              <Cell label={t.chart.colHouse} className="py-2 pr-3 tabular-nums">
                {/*
                  An em dash, never "0th". Without a birth time there is
                  no house to be in, and the reader is told why once at
                  the top of the screen rather than nine times here.
                */}
                {planet.house === null ? '—' : ordinal(planet.house)}
              </Cell>

              <Cell label={t.chart.colNakshatra} className="py-2 pr-3">
                <AstroTerm term="nakshatra">
                  {formatNakshatra(planet.nakshatra, planet.pada)}
                </AstroTerm>
              </Cell>

              <Cell label={t.chart.colStatus} className="py-2">
                <Status planet={planet} />
              </Cell>
            </tr>
          ))}
        </tbody>
      </table>

      <PlanetSheet planet={open} onClose={() => setOpen(null)} />
    </>
  )
}

function Cell({
  label,
  className,
  children,
}: {
  label: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <td
      /*
        jsx-a11y maps <td> to `gridcell`, which is interactive, and so
        reads `cell` as a downgrade. It is not one — `cell` is the
        correct role for a cell in a plain table — and it is stated
        explicitly because the mobile restack below sets `display: flex`
        here, which drops the element from the table mapping entirely in
        Chromium and WebKit. The alternative the rule steers towards, no
        role at all, is the broken case this is defending against.

        Disabled for the attribute rather than the file: an actual
        interactive element given a non-interactive role anywhere else in
        here should still fail.
      */
      // eslint-disable-next-line jsx-a11y/no-interactive-element-to-noninteractive-role
      role="cell"
      data-label={label}
      className={cn(
        // At mobile the column name is drawn from the attribute. It is
        // decorative — the header cell above still carries it for
        // assistive technology, so repeating it here would say
        // everything twice.
        //
        // Flex with a fixed-width label, not subgrid: subgrid needs the
        // row to be a grid whose tracks these cells can join, which the
        // block restack above deliberately is not, and its browser floor
        // is higher than anything else this app relies on.
        'max-md:flex max-md:gap-3 max-md:py-1',
        'max-md:before:w-24 max-md:before:shrink-0 max-md:before:text-ink-muted',
        'max-md:before:content-[attr(data-label)]',
        className,
      )}
    >
      {children}
    </td>
  )
}

/**
 * Retrograde and combustion, as words and marks — never as colour.
 *
 * Both change how a placement is read. `℞` is the conventional mark and
 * carries an accessible name, so the information survives greyscale,
 * colour blindness and a screen reader equally.
 */
function Status({ planet }: { planet: PlanetPlacement }) {
  const d = dignity(planet.dignity)

  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {d.term ? <AstroTerm term={d.term}>{d.label}</AstroTerm> : <span>{d.label}</span>}

      {planet.isRetrograde && (
        <AstroTerm term="retrograde" className="text-gold">
          <span aria-hidden="true">{RETROGRADE_MARK}</span>
          <span className="sr-only">retrograde</span>
        </AstroTerm>
      )}

      {planet.isCombust && <AstroTerm term="combust">Combust</AstroTerm>}
    </span>
  )
}

function PlanetSheet({
  planet,
  onClose,
}: {
  planet: PlanetPlacement | null
  onClose: () => void
}) {
  if (!planet) return null

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{planet.planet}</DialogTitle>
          <DialogDescription>
            {planet.sign} {formatDegree(planet.degree)}
            {planet.house !== null && ` · ${ordinal(planet.house)} house`}
          </DialogDescription>
        </DialogHeader>

        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-ink-muted">Nakshatra</dt>
          <dd>{formatNakshatra(planet.nakshatra, planet.pada)}</dd>

          <dt className="text-ink-muted">Dignity</dt>
          <dd>{dignity(planet.dignity).label}</dd>

          <dt className="text-ink-muted">Motion</dt>
          <dd>{planet.isRetrograde ? `Retrograde ${RETROGRADE_MARK}` : 'Direct'}</dd>

          <dt className="text-ink-muted">Combust</dt>
          <dd>{planet.isCombust ? 'Yes' : 'No'}</dd>
        </dl>
      </DialogContent>
    </Dialog>
  )
}
