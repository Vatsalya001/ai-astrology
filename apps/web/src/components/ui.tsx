import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/**
 * App-specific presentational components.
 *
 * These are deliberately NOT named `Button` or `Card`: those live in
 * `@/components/ui/*` (shadcn). Having both a `ui.tsx` file and a `ui/`
 * directory export the same name is a trap — `@/components/ui` resolves
 * to the file and `@/components/ui/button` into the directory, so an
 * import can silently pick up the wrong component with no type error.
 *
 * The padded surface below is a `Panel` for that reason. shadcn's `Card`
 * composes with `CardHeader`/`CardContent` and carries no padding of its
 * own; this one is a single padded block, which is what every Phase 0
 * page actually wants.
 */

// ─── Panel ───────────────────────────────────────────────────────────

export function Panel({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return <div className={cn('card p-6', className)}>{children}</div>
}

// ─── Badge ───────────────────────────────────────────────────────────

const BADGE_TONES = {
  neutral: 'border-border bg-surface text-ink-muted',
  accent: 'border-accent/40 bg-accent/10 text-accent-soft',
  gold: 'border-gold/35 bg-gold/10 text-gold-soft',
  ok: 'border-ok/35 bg-ok/10 text-ok',
  warn: 'border-warn/35 bg-warn/10 text-warn',
  danger: 'border-danger/35 bg-danger/10 text-danger',
} as const

export function Badge({
  children,
  tone = 'neutral',
  className,
}: {
  children: ReactNode
  tone?: keyof typeof BADGE_TONES
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium',
        BADGE_TONES[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}

// ─── Status dot ──────────────────────────────────────────────────────

/**
 * Status is conveyed by shape/label as well as colour.
 *
 * Colour alone fails for the ~8% of men with red-green colour blindness,
 * which is exactly the population most likely to be reading an ops
 * dashboard.
 */
export function StatusDot({
  status,
}: {
  status: 'ok' | 'error' | 'degraded' | 'pending'
}) {
  const tone = {
    ok: 'bg-ok',
    error: 'bg-danger',
    degraded: 'bg-warn',
    pending: 'bg-ink-faint',
  }[status]

  return (
    <span className="relative inline-flex h-2.5 w-2.5 shrink-0" aria-hidden="true">
      {status === 'ok' && (
        <span
          className={cn('absolute inline-flex h-full w-full rounded-full opacity-60', tone)}
          style={{ animation: 'pulse-ring 2s cubic-bezier(0.4,0,0.6,1) infinite' }}
        />
      )}
      <span className={cn('relative inline-flex h-2.5 w-2.5 rounded-full', tone)} />
    </span>
  )
}

// ─── Section heading ─────────────────────────────────────────────────

export function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <p className="mb-3 text-xs font-medium uppercase tracking-[0.18em] text-ink-faint">
      {children}
    </p>
  )
}
