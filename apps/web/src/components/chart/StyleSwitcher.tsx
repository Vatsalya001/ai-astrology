'use client'

import type { ChartStyle } from '@ayana/astrology-geometry'
import { defineTerm, type GlossaryKey } from '@ayana/content'

import { AstroTerm } from '@/components/AstroTerm'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

import { CHART_STYLES } from './style'

/**
 * North Indian or South Indian.
 *
 * ── Why this persists to the server ──
 *
 * The spec is explicit that the two styles are not cosmetic: a North
 * Indian reader expects a diamond and a South Indian reader expects a
 * grid, and showing the wrong one makes the product feel foreign. That
 * makes it a real preference rather than a view toggle, so it is stored
 * on the account and follows the reader to a new device — which is also
 * what the gate item asks for ("switcher persists to preferences").
 *
 * The caller owns the write. This component reports the change and
 * stays a controlled input; putting the API call in here would make it
 * unusable in the PDF worker and in Storybook.
 *
 * Real radios and a non-colour selection marker, for the same reasons as
 * `VargaSwitcher` — see its header.
 */
/**
 * `GlossaryKey`, not `string`.
 *
 * It reaches `AstroTerm` as a computed prop, which the source scan in
 * `astro-term-usage.test.ts` cannot follow — and an unresolved term
 * renders as plain text with nothing to report it. The type carries the
 * guarantee; `StyleSwitcher.test.tsx` checks the mapping, because a
 * well-typed key can still point at the wrong entry.
 */
export const STYLE_TERMS: Record<ChartStyle, GlossaryKey> = {
  north: 'chart_style_north',
  south: 'chart_style_south',
}

const STYLE_LABELS = {
  north: 'north',
  south: 'south',
} as const

export function StyleSwitcher({
  value,
  onChange,
  disabled = false,
  className,
}: {
  value: ChartStyle
  onChange: (value: ChartStyle) => void
  disabled?: boolean
  className?: string
}) {
  const { t, locale } = useLocale()

  return (
    <fieldset className={cn('min-w-0', className)} disabled={disabled}>
      <legend className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
        {t.chart.styleLegend}
      </legend>

      <div className="flex flex-wrap gap-2">
        {CHART_STYLES.map((style) => {
          const selected = style === value
          return (
            <label
              key={style}
              className={cn(
                'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm',
                'transition-colors focus-within:ring-2 focus-within:ring-ring',
                'focus-within:ring-offset-2 focus-within:ring-offset-background',
                selected
                  ? 'border-gold bg-gold/10 text-ink'
                  : 'border-input text-ink-muted hover:border-border',
                disabled && 'cursor-not-allowed opacity-60',
              )}
            >
              <input
                type="radio"
                name="chart-style"
                value={style}
                checked={selected}
                onChange={() => onChange(style)}
                className="sr-only"
              />
              {/*
                A filled dot as well as the gold tint. Around 8% of men
                cannot separate the two borders, and a control whose
                state only they cannot read is one that silently stops
                working for them.
              */}
              <span
                aria-hidden="true"
                className={cn(
                  'h-2 w-2 shrink-0 rounded-full border',
                  selected ? 'border-gold bg-gold' : 'border-input bg-transparent',
                )}
              />
              <span className="font-medium">{t.values[STYLE_LABELS[style]]}</span>
            </label>
          )
        })}
      </div>

      {/*
        The one-line explanation inline, and the name tappable for the
        rest — the same shape as VargaSwitcher.

        "North" on its own tells a reader nothing about why the diagram
        changed shape, and the two layouts look nothing like each other.
        The text comes from the glossary rather than the dictionary so
        there is one description of a North Indian chart in the product,
        not two that can drift.
      */}
      <p className="mt-2 text-xs leading-relaxed text-ink-muted" aria-live="polite">
        <AstroTerm term={STYLE_TERMS[value]} />
        {styleSummary(value, locale) && <> — {styleSummary(value, locale)}</>}
      </p>
    </fieldset>
  )
}

/** The glossary's one-liner for a style, or nothing if it has none. */
function styleSummary(style: ChartStyle, locale: 'en' | 'hi'): string {
  return defineTerm(STYLE_TERMS[style], locale)?.short ?? ''
}
