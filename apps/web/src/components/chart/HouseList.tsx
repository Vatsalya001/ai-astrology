'use client'

import { useState } from 'react'

import type { GlossaryKey } from '@ayana/content'

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

import { formatDegree } from './format'
import { ordinal, planetAbbreviation } from './glyphs'
import type { HousePlacement, PlanetPlacement } from './types'

/**
 * The twelve houses, each with its sign, its lord and who is sitting in
 * it.
 *
 * ── Why occupants are computed here and not asked for ──
 *
 * `astro-service` returns planets with a `house` field and houses
 * without an occupant list. Grouping in the component rather than adding
 * a field keeps one source of truth: if the two ever disagreed, a
 * planet could appear in the 5th house in the table and the 6th in this
 * list, and nothing would be obviously wrong on either screen.
 *
 * ── Why a list and not a second chart ──
 *
 * The diagram already shows the houses spatially. This screen exists for
 * the reader who wants to go house by house and be told what each one
 * covers, which a diamond cannot do. Each house name is a glossary term,
 * so "what is the 7th house even for" is one tap and not a search.
 */

/**
 * The classical name of each house, as a glossary key.
 *
 * Indexed by house number, so index 0 is unused — houses are 1–12 and an
 * off-by-one here labels every house with its neighbour's meaning, which
 * is the kind of wrong that looks entirely plausible on screen.
 *
 * It happened while writing this: the corpus had eleven `*_bhava` terms
 * and no third house, so index 3 briefly pointed at `sukha_bhava` — the
 * FOURTH house — and the 3rd house rendered "Home, mother, comfort".
 * Nothing about that looks like a bug. `HouseList.test.tsx` now asserts
 * all twelve are present, distinct, and named for the house they are on.
 */
export const HOUSE_TERMS: readonly (GlossaryKey | null)[] = [
  null,
  'lagna_bhava',
  'dhana_bhava',
  'sahaja_bhava',
  'sukha_bhava',
  'putra_bhava',
  'ripu_bhava',
  'kalatra_bhava',
  'ayur_bhava',
  'bhagya_bhava',
  'karma_bhava',
  'labha_bhava',
  'vyaya_bhava',
] as const

export function HouseList({
  houses,
  planets,
  className,
}: {
  /** Null when the birth time is unknown — there are no houses then. */
  houses: HousePlacement[] | null
  planets: PlanetPlacement[]
  className?: string
}) {
  const { t, fill } = useLocale()
  const [open, setOpen] = useState<HousePlacement | null>(null)

  /*
    Not an empty list and not an empty state that says "no houses".
    Without a birth time the ascendant is a guess, so the houses are not
    missing data — they are undefined, and saying which is the difference
    between a reader who fixes their birth time and one who thinks the
    app failed to load.
  */
  if (houses === null) {
    return (
      <p
        className={cn('rounded-lg border border-border p-6 text-sm text-ink-muted', className)}
      >
        {t.chart.housesNoBirthTime}
      </p>
    )
  }

  /*
    A planet with no house is not in house 0, it is in no house — this
    branch is only reachable when `houses` is non-null, so in practice
    it never fires, and it is written anyway because "unreachable" is a
    claim about today's callers rather than about the type.
  */
  const occupants = new Map<number, PlanetPlacement[]>()
  for (const planet of planets) {
    if (planet.house === null) continue
    const list = occupants.get(planet.house)
    if (list) list.push(planet)
    else occupants.set(planet.house, [planet])
  }

  return (
    <>
      <ul className={cn('divide-y divide-border', className)}>
        {houses.map((house) => {
          const here = occupants.get(house.house) ?? []
          const term = HOUSE_TERMS[house.house] ?? null

          return (
            <li key={house.house}>
              <button
                type="button"
                onClick={() => setOpen(house)}
                className={cn(
                  'flex w-full items-baseline gap-3 py-3 text-left text-sm',
                  'hover:bg-elevated focus-visible:outline-none focus-visible:ring-2',
                  'focus-visible:ring-ring focus-visible:ring-offset-2',
                  'focus-visible:ring-offset-background rounded-sm px-2',
                )}
                /*
                  Spelled out, because the visual row reads as
                  "7th · Libra · lord Venus · 2" — four fragments that a
                  screen reader runs together into something that is not
                  a sentence. The occupants are named rather than counted
                  for the same reason.
                */
                aria-label={[
                  fill(t.chart.houseNumbered, { ordinal: ordinal(house.house) }),
                  house.sign,
                  `ruled by ${house.lord}`,
                  here.length === 0 ? t.chart.houseEmpty : here.map((p) => p.planet).join(', '),
                ].join(', ')}
              >
                <span className="w-10 shrink-0 tabular-nums text-ink-muted">
                  {ordinal(house.house)}
                </span>

                <span className="min-w-0 flex-1">
                  <span className="font-medium text-ink">{house.sign}</span>
                  <span className="text-ink-muted"> · lord {house.lord}</span>

                  {here.length > 0 && (
                    <span className="mt-0.5 block text-ink-muted">
                      {here.map((p) => p.planet).join(', ')}
                    </span>
                  )}
                </span>

                {/*
                  The count is repeated as a number rather than shown only
                  by the names above, so a reader scanning the column can
                  see at a glance which houses are occupied. Empty houses
                  say "empty" rather than showing nothing — a blank cell
                  is indistinguishable from a rendering failure.
                */}
                <span className="shrink-0 text-xs text-ink-muted">
                  {here.length === 0 ? t.chart.houseEmpty : `${here.length}`}
                </span>
              </button>

              {term && (
                <p className="px-2 pb-3 text-xs text-ink-muted">
                  <AstroTerm term={term} />
                </p>
              )}
            </li>
          )
        })}
      </ul>

      <HouseSheet
        house={open}
        occupants={open ? (occupants.get(open.house) ?? []) : []}
        onClose={() => setOpen(null)}
      />
    </>
  )
}

function HouseSheet({
  house,
  occupants,
  onClose,
}: {
  house: HousePlacement | null
  occupants: PlanetPlacement[]
  onClose: () => void
}) {
  const { t, fill } = useLocale()
  if (!house) return null

  const term = HOUSE_TERMS[house.house] ?? null

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{fill(t.chart.houseNumbered, { ordinal: ordinal(house.house) })}</DialogTitle>
          <DialogDescription>
            {house.sign}, ruled by {house.lord}
          </DialogDescription>
        </DialogHeader>

        {term && (
          <p className="text-sm text-ink-muted">
            <AstroTerm term={term} />
          </p>
        )}

        <div className="text-sm">
          <h3 className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
            {t.chart.housePlanetsHere}
          </h3>
          {occupants.length === 0 ? (
            <p className="text-ink-muted">
              {fill(t.chart.houseNoPlanets, { lord: house.lord })}
            </p>
          ) : (
            <ul className="space-y-1">
              {occupants.map((planet) => (
                <li key={planet.planet}>
                  <span aria-hidden="true" className="mr-1.5 text-ink-muted">
                    {planetAbbreviation(planet.planet)}
                  </span>
                  {planet.planet} · {planet.sign} {formatDegree(planet.degree)}
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
