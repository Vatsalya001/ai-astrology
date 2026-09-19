'use client'

import { cn } from '@/lib/utils'

/**
 * Progress through a multi-step form.
 *
 * Both a bar and a sentence. The bar alone is a colour carrying meaning,
 * which is exactly what the accessibility rules forbid — the text is
 * what a screen reader reads and what somebody who cannot distinguish
 * the filled segments from the empty ones relies on.
 */
export function StepHeader({
  current,
  total,
  onBack,
  label,
}: {
  current: number
  total: number
  onBack: () => void
  label: string
}) {
  return (
    <div>
      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={onBack}
          className="-ml-2 rounded px-2 py-1 text-sm text-ink-muted transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <span aria-hidden="true">←</span> Back
        </button>

        <p className="text-sm text-ink-muted tabular-nums">
          {label.replace('{current}', String(current)).replace('{total}', String(total))}
        </p>
      </div>

      {/* aria-hidden because the sentence above already says it. A
          progressbar role here would have a screen reader announce the
          same fact twice, once as prose and once as a percentage. */}
      <div aria-hidden="true" className="mt-3 flex gap-1.5">
        {Array.from({ length: total }, (_, index) => (
          <div
            key={index}
            className={cn(
              'h-1 flex-1 rounded-full transition-colors',
              index < current ? 'bg-gold' : 'bg-elevated',
            )}
          />
        ))}
      </div>
    </div>
  )
}
