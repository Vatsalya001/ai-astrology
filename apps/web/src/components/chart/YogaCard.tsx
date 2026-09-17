'use client'

import { useState } from 'react'

import { describeYoga } from '@ayana/content'

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

import { ordinal } from './glyphs'
import type { YogaPlacement } from './types'

/**
 * One detected planetary combination.
 *
 * ── Strength is a word, and stays a word ──
 *
 * `astro-service` returns `strong` or `moderate` and says why in its own
 * schema: "never a number — a score would imply a precision the
 * tradition does not have and the product could not defend". A five-star
 * rating or a percentage bar would reintroduce exactly that precision
 * wearing a costume, and it is the single most tempting thing to build
 * on this card. So the word is printed, and `YogaCard.test.tsx` asserts
 * no digit ever appears next to it.
 *
 * ── The description is static, never generated ──
 *
 * Which combination was found is computed; what it means is written.
 * Nothing here composes a sentence from the planets and houses — that
 * would be the frontend generating interpretation, which is the same
 * rule that keeps the model out of the chart. An undescribed yoga shows
 * its name and its placements and no paragraph, because a heading over
 * invented filler is worse than a heading over nothing.
 */
export function YogaCard({
  yoga,
  className,
}: {
  yoga: YogaPlacement
  className?: string
}) {
  const { locale, t, fill } = useLocale()
  const [open, setOpen] = useState(false)

  const described = describeYoga(yoga.name, locale)
  const strong = yoga.strength === 'strong'

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={cn(
          'w-full rounded-lg border border-border p-4 text-left transition-colors',
          'hover:border-gold/40 hover:bg-elevated',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          'focus-visible:ring-offset-2 focus-visible:ring-offset-background',
          className,
        )}
        aria-label={fill(t.chart.yogaCardLabel, {
          name: described?.name ?? yoga.name,
          strength: yoga.strength,
          planets: yoga.involvedPlanets.join(' and '),
        })}
      >
        <span className="flex items-baseline justify-between gap-3">
          <span className="font-medium text-ink">{described?.name ?? yoga.name}</span>

          {/*
            The word, with a dot beside it rather than instead of it.
            Gold against grey is not enough on its own, and there is
            nothing to scale here anyway — two values do not want a bar.
          */}
          <span
            className={cn(
              'flex shrink-0 items-center gap-1.5 text-xs',
              strong ? 'text-gold' : 'text-ink-muted',
            )}
          >
            <span
              aria-hidden="true"
              className={cn(
                'h-1.5 w-1.5 rounded-full',
                strong ? 'bg-gold' : 'bg-ink-muted/60',
              )}
            />
            {strong ? t.chart.yogaStrong : t.chart.yogaModerate}
          </span>
        </span>

        {described ? (
          <span className="mt-1 block text-sm text-ink-muted">{described.short}</span>
        ) : (
          /*
            No description for a name the engine sent. Says so rather
            than leaving a blank that reads as a rendering failure — and
            `yogas.test.ts` turns this state into a failing build, so it
            should never be seen.
          */
          <span className="mt-1 block text-sm text-ink-muted">
            {t.chart.yogaNoDescription}
          </span>
        )}

        {(yoga.involvedPlanets.length > 0 || yoga.involvedHouses.length > 0) && (
          <span className="mt-2 block text-xs text-ink-muted">
            {yoga.involvedPlanets.join(', ')}
            {yoga.involvedPlanets.length > 0 && yoga.involvedHouses.length > 0 && ' · '}
            {yoga.involvedHouses.map((h) => ordinal(h)).join(', ')}
            {yoga.involvedHouses.length > 0 && ` ${t.chart.colHouse.toLowerCase()}`}
            {yoga.involvedHouses.length > 1 && 's'}
          </span>
        )}
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{described?.name ?? yoga.name}</DialogTitle>
            <DialogDescription>
              {strong ? t.chart.yogaStrong : t.chart.yogaModerate}
              {yoga.involvedPlanets.length > 0 && ` · ${yoga.involvedPlanets.join(', ')}`}
            </DialogDescription>
          </DialogHeader>

          {described && (
            <p className="text-sm leading-relaxed text-foreground">{described.long}</p>
          )}

          <p className="text-xs text-ink-muted">
            <AstroTerm term="yoga" />
          </p>
        </DialogContent>
      </Dialog>
    </>
  )
}
