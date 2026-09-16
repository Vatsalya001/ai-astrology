'use client'

import { AstroTerm } from '@/components/AstroTerm'
import { VARGAS, type VargaType } from '@/lib/varga'
import { cn } from '@/lib/utils'

/**
 * D1 / D9 / D10.
 *
 * ── Real radios, not buttons with aria-pressed ──
 *
 * Three mutually exclusive options is what a radio group is. Using real
 * inputs inside a `fieldset` means arrow-key navigation, the roving
 * tab stop and the checked state all come from the browser rather than
 * from hand-written key handlers that have to be maintained and tested.
 * The inputs are `sr-only`; the labels carry the visuals.
 *
 * ── Selection is not signalled by colour alone ──
 *
 * The selected option gets a gold border and tint, and ALSO a filled
 * dot. Around 8% of men cannot separate the gold from the grey, and a
 * segmented control whose state only they cannot read is a control that
 * silently stops working for them. The dot is `aria-hidden` — assistive
 * technology already has the radio's checked state and does not need it
 * said twice.
 */
export function VargaSwitcher({
  value,
  onChange,
  disabled = false,
  className,
}: {
  value: VargaType
  onChange: (value: VargaType) => void
  disabled?: boolean
  className?: string
}) {
  return (
    <fieldset className={cn('min-w-0', className)} disabled={disabled}>
      <legend className="mb-2 text-xs uppercase tracking-wide text-ink-muted">
        Chart
      </legend>

      <div className="flex flex-wrap gap-2">
        {VARGAS.map((v) => {
          const selected = v.type === value
          return (
            <label
              key={v.type}
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
                name="varga"
                value={v.type}
                checked={selected}
                onChange={() => onChange(v.type)}
                className="sr-only"
              />
              <span
                aria-hidden="true"
                className={cn(
                  'h-2 w-2 shrink-0 rounded-full border',
                  selected ? 'border-gold bg-gold' : 'border-input bg-transparent',
                )}
              />
              <span className="font-medium">{v.name}</span>
              <span className="text-xs text-ink-muted">{v.type}</span>
            </label>
          )
        })}
      </div>

      {/*
        The purpose of whichever chart is showing, under the control.
        "D9" means nothing on first sight, and a reader who presses it,
        sees a completely different diagram and has no idea why concludes
        the app is broken. This is one line and it stops that.

        `aria-live` because the text changes without the focus moving to
        it — a screen reader user selecting Navamsa should hear what
        Navamsa is read for, not silence.
      */}
      <p className="mt-2 text-xs leading-relaxed text-ink-muted" aria-live="polite">
        <AstroTerm term={selectedVarga(value).term}>{selectedVarga(value).name}</AstroTerm>
        {' — '}
        {selectedVarga(value).purpose}
      </p>
    </fieldset>
  )
}

/**
 * Falls back to the rasi rather than throwing.
 *
 * `value` is typed, but it arrives from a URL parameter or stored
 * preference in practice. A chart the list has never heard of should
 * show the birth chart, not a blank panel or a crash.
 */
function selectedVarga(value: string) {
  return VARGAS.find((v) => v.type === value) ?? VARGAS[0]!
}
