import type { ReactNode } from 'react'

/** Tiny class-name joiner. Avoids pulling in a dependency for this. */
export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}

// ─── Button ──────────────────────────────────────────────────────────

type ButtonProps = {
  children: ReactNode
  variant?: 'primary' | 'secondary' | 'ghost'
  href?: string
  className?: string
  disabled?: boolean
  title?: string
}

const BUTTON_BASE =
  'inline-flex items-center justify-center gap-2 rounded-xl px-5 py-3 text-sm ' +
  'font-medium transition-all duration-200 disabled:cursor-not-allowed disabled:opacity-45'

const BUTTON_VARIANTS = {
  primary:
    'bg-gold text-base hover:bg-gold-soft hover:shadow-[0_0_28px_-6px_rgba(212,168,87,0.55)]',
  secondary:
    'border border-border bg-surface text-ink hover:border-accent-soft hover:bg-elevated',
  ghost: 'text-ink-muted hover:text-ink hover:bg-surface',
} as const

export function Button({
  children,
  variant = 'primary',
  href,
  className,
  disabled,
  title,
}: ButtonProps) {
  const classes = cx(BUTTON_BASE, BUTTON_VARIANTS[variant], className)

  if (href && !disabled) {
    return (
      <a href={href} className={classes} title={title}>
        {children}
      </a>
    )
  }

  return (
    <button type="button" className={classes} disabled={disabled} title={title}>
      {children}
    </button>
  )
}

// ─── Card ────────────────────────────────────────────────────────────

export function Card({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return <div className={cx('card p-6', className)}>{children}</div>
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
      className={cx(
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
          className={cx('absolute inline-flex h-full w-full rounded-full opacity-60', tone)}
          style={{ animation: 'pulse-ring 2s cubic-bezier(0.4,0,0.6,1) infinite' }}
        />
      )}
      <span className={cx('relative inline-flex h-2.5 w-2.5 rounded-full', tone)} />
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
