'use client'

import { cn } from '@/lib/utils'

/**
 * The AI chat entry point, present and disabled.
 *
 * ── Why this ships before it works ──
 *
 * The spec's reasoning, and it is worth restating because a disabled
 * control is otherwise indefensible: placing it now means Phase 5 ships
 * a working feature, rather than a working feature PLUS a navigation
 * redesign that has to be reviewed, tested and explained all over again.
 * The shape of the dashboard is settled while the stakes are low.
 *
 * ── Disabled honestly ──
 *
 * The four topic chips and the input are really disabled — not styled to
 * look inert while remaining focusable, which is the usual way this goes
 * wrong. A keyboard user tabbing into a box that swallows their question
 * has been lied to more completely than one who never reached it.
 *
 * `aria-describedby` points at the explanation, so a screen reader that
 * lands here is told WHY rather than just "dimmed, edit text, disabled".
 */
const TOPICS = ['Career', 'Love', 'Money', 'Marriage'] as const

export function AskBox({
  enabled,
  className,
}: {
  enabled: boolean
  className?: string
}) {
  return (
    <section
      aria-labelledby="ask-heading"
      /*
        No opacity on the section.

        It was `opacity-70` on the whole card, which dragged the
        explanatory note below 4.5:1 and axe caught it — the one piece
        of text that MUST stay readable when the box is disabled is the
        sentence saying why it is disabled. The controls below carry
        their own dimming instead.
      */
      className={cn('rounded-lg border border-border p-4', className)}
    >
      <h2 id="ask-heading" className="font-serif text-base text-ink">
        Ask your AI astrologer
      </h2>

      <input
        type="text"
        disabled={!enabled}
        placeholder="What&rsquo;s on your mind?"
        aria-label="Ask your AI astrologer"
        aria-describedby={enabled ? undefined : 'ask-disabled-note'}
        className={cn(
          'mt-3 w-full rounded-lg border border-input bg-surface px-3 py-2 text-sm',
          'placeholder:text-ink-faint focus-visible:outline-none focus-visible:ring-2',
          'focus-visible:ring-ring focus-visible:ring-offset-2',
          'focus-visible:ring-offset-background',
          !enabled && 'cursor-not-allowed opacity-60',
        )}
      />

      <div className="mt-3 flex flex-wrap gap-2">
        {TOPICS.map((topic) => (
          <button
            key={topic}
            type="button"
            disabled={!enabled}
            aria-describedby={enabled ? undefined : 'ask-disabled-note'}
            className={cn(
              'rounded-full border border-input px-3 py-1 text-xs text-ink-muted',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              'focus-visible:ring-offset-2 focus-visible:ring-offset-background',
              enabled ? 'hover:border-gold hover:text-ink' : 'cursor-not-allowed opacity-60',
            )}
          >
            {topic}
          </button>
        ))}
      </div>

      {!enabled && (
        <p id="ask-disabled-note" className="mt-3 text-xs text-ink">
          Not available yet. Everything on this screen so far is computed from your chart,
          with no AI involved — that part comes later.
        </p>
      )}
    </section>
  )
}
