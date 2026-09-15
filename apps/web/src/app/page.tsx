import Link from 'next/link'
import { Starfield } from '@/components/Starfield'
import { Wordmark } from '@/components/Logo'
import { Badge, Panel, SectionLabel } from '@/components/ui'
import { Button } from '@/components/ui/button'
import { getDictionary } from '@/lib/i18n/dictionaries'

/**
 * Reads the dictionary directly rather than through `useLocale`.
 *
 * This is a Server Component and `useLocale` is a client hook — making
 * the whole marketing page a client component to reach it would trade
 * static rendering for a language switch nobody uses before signing in.
 * Server-rendered English is the right default here; the app switches
 * language once there is an account to read the preference from.
 */
const t = getDictionary('en')

export default function LandingPage() {
  return (
    <>
      <SiteHeader />
      <main id="main">
        <Hero />
        <HowItWorks />
        <Principles />
        <BuildStatus />
      </main>
      <SiteFooter />
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────

function SiteHeader() {
  return (
    <header className="relative z-20 border-b border-border/60">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-5">
        <Wordmark />
        <nav className="flex items-center gap-2">
          <Link
            href="/status"
            className="rounded-lg px-3 py-2 text-sm text-ink-muted transition-colors hover:text-ink"
          >
            {t.landing.systemStatus}
          </Link>
          <Button variant="secondary" size="sm" asChild>
            <Link href="/auth">Sign in</Link>
          </Button>
        </nav>
      </div>
    </header>
  )
}

function Hero() {
  return (
    <section className="relative overflow-hidden">
      <Starfield />
      <div className="pointer-events-none absolute inset-0 bg-radial-glow" />

      {/* z-10 is explicit rather than relying on paint order: the
          starfield must never sit on top of the headline. */}
      <div className="relative z-10 mx-auto max-w-6xl px-6 pb-24 pt-24 sm:pt-32">
        <div className="mx-auto max-w-3xl text-center">
          <Badge tone="accent" className="animate-fade-up">
            <span className="relative flex h-1.5 w-1.5">
              <span className="absolute inline-flex h-full w-full rounded-full bg-accent-soft opacity-75" />
              <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-accent-soft" />
            </span>
            {t.landing.phase}
          </Badge>

          <h1
            className="mt-8 animate-fade-up text-balance font-serif text-5xl leading-[1.08] tracking-tight sm:text-6xl md:text-7xl"
            style={{ animationDelay: '80ms' }}
          >
            {t.landing.headlineA}
            <span className="mt-2 block bg-gradient-to-r from-gold-soft via-gold to-gold-dim bg-clip-text text-transparent">
              {t.landing.headlineB}
            </span>
          </h1>

          <p
            className="mx-auto mt-7 max-w-2xl animate-fade-up text-balance text-lg leading-relaxed text-ink-muted"
            style={{ animationDelay: '160ms' }}
          >
            {t.landing.lede}
          </p>

          <div
            className="mt-10 flex animate-fade-up flex-col items-center justify-center gap-3 sm:flex-row"
            style={{ animationDelay: '240ms' }}
          >
            <Button size="lg" asChild>
              <Link href="/auth">{t.landing.ctaPrimary}</Link>
            </Button>
            <Button size="lg" variant="secondary" asChild>
              <Link href="/status">{t.landing.ctaSecondary}</Link>
            </Button>
          </div>

          <p
            className="mt-5 animate-fade-up text-xs text-ink-faint"
            style={{ animationDelay: '320ms' }}
          >
            {t.landing.ctaNote}
          </p>
        </div>
      </div>

      <div className="hairline" />
    </section>
  )
}

const STEPS = [
  {
    n: '01',
    title: 'Your birth details',
    body: 'Date, time and place. We resolve the exact historical timezone offset — a 30-minute error can change your entire chart.',
    phase: 'Phase 2',
  },
  {
    n: '02',
    title: 'Your chart, computed',
    body: 'Swiss Ephemeris calculates every placement, dasha and transit. Deterministic maths — never a language model guessing.',
    phase: 'Phase 2',
  },
  {
    n: '03',
    title: 'Ask anything',
    body: 'Chat about career, marriage, money or the year ahead. Every answer is grounded in your real placements, and shows its working.',
    phase: 'Phase 5',
  },
  {
    n: '04',
    title: 'Talk to a human',
    body: 'When you want a real astrologer, hand off with full context — and only what you explicitly choose to share.',
    phase: 'Phase 8',
  },
]

function HowItWorks() {
  return (
    <section className="mx-auto max-w-6xl px-6 py-24">
      <div className="mb-14 text-center">
        <SectionLabel>{t.landing.howLabel}</SectionLabel>
        <h2 className="font-serif text-4xl tracking-tight">
          {t.landing.howTitle}
        </h2>
      </div>

      <ol className="grid gap-5 md:grid-cols-2 lg:grid-cols-4">
        {STEPS.map((step) => (
          <li key={step.n}>
            <Panel className="group h-full transition-colors duration-300 hover:border-accent/45">
              <div className="mb-4 flex items-start justify-between">
                <span className="font-mono text-sm text-gold">{step.n}</span>
                <Badge tone="neutral" className="text-[10px]">
                  {step.phase}
                </Badge>
              </div>
              <h3 className="mb-2 font-serif text-xl">{step.title}</h3>
              <p className="text-sm leading-relaxed text-ink-muted">{step.body}</p>
            </Panel>
          </li>
        ))}
      </ol>
    </section>
  )
}

const PRINCIPLES = [
  {
    title: 'Astrology is computed, never guessed',
    body: 'Planetary positions, houses, nakshatras and dashas come from a dedicated service with no access to any language model. The AI interprets. It never calculates.',
  },
  {
    title: 'Every claim shows its working',
    body: 'Tap "Why am I seeing this?" on any reading and see exactly which house, planet, dasha and transit it was built from — highlighted on your own chart.',
  },
  {
    title: 'Guidance, not certainty',
    body: 'No guaranteed outcomes about marriage, health, money or anything else. Traditional framing, honestly presented.',
  },
  {
    title: 'Your data stays yours',
    body: 'See everything the AI remembers about you, edit it, or delete it. Real account deletion means the rows are gone, not flagged.',
  },
]

function Principles() {
  return (
    <section className="border-y border-border/60 bg-surface/25">
      <div className="mx-auto max-w-6xl px-6 py-24">
        <div className="mb-14 text-center">
          <SectionLabel>{t.landing.commitLabel}</SectionLabel>
          <h2 className="font-serif text-4xl tracking-tight">
            {t.landing.commitTitle}
          </h2>
        </div>

        <div className="grid gap-5 md:grid-cols-2">
          {PRINCIPLES.map((p) => (
            <Panel key={p.title} className="border-border/70">
              <div className="mb-3 flex items-center gap-2.5">
                <span className="text-gold" aria-hidden="true">
                  ✦
                </span>
                <h3 className="font-serif text-xl">{p.title}</h3>
              </div>
              <p className="text-sm leading-relaxed text-ink-muted">{p.body}</p>
            </Panel>
          ))}
        </div>
      </div>
    </section>
  )
}

const ROADMAP = [
  { phase: 'Phase 0', name: 'Foundation', state: 'done' },
  { phase: 'Phase 1', name: 'Accounts', state: 'active' },
  { phase: 'Phase 2', name: 'Birth charts', state: 'next' },
  { phase: 'Phase 3', name: 'Kundli UI', state: 'planned' },
  { phase: 'Phase 4', name: 'AI core', state: 'planned' },
  { phase: 'Phase 5', name: 'Chat', state: 'planned' },
  { phase: 'Phase 6', name: 'Memory', state: 'planned' },
] as const

function BuildStatus() {
  return (
    <section className="mx-auto max-w-6xl px-6 py-24">
      <div className="mb-12 text-center">
        <SectionLabel>{t.landing.progressLabel}</SectionLabel>
        <h2 className="font-serif text-4xl tracking-tight">{t.landing.progressTitle}</h2>
        <p className="mx-auto mt-3 max-w-xl text-sm text-ink-muted">
          {t.landing.progressBody}
        </p>
      </div>

      <ol className="flex flex-wrap items-center justify-center gap-2">
        {ROADMAP.map((r) => (
          <li key={r.phase}>
            <div
              className={[
                'rounded-xl border px-4 py-3 text-center transition-colors',
                r.state === 'active'
                  ? 'border-gold/50 bg-gold/10'
                  : r.state === 'done'
                    ? 'border-ok/35 bg-ok/[0.06]'
                    : r.state === 'next'
                      ? 'border-accent/35 bg-accent/[0.06]'
                      : 'border-border bg-surface/45',
              ].join(' ')}
            >
              <p
                className={[
                  'font-mono text-[11px] uppercase tracking-wider',
                  r.state === 'active'
                    ? 'text-gold'
                    : r.state === 'done'
                      ? 'text-ok'
                      : 'text-ink-faint',
                ].join(' ')}
              >
                {r.phase}
              </p>
              <p
                className={[
                  'mt-0.5 text-sm',
                  r.state === 'planned' ? 'text-ink-faint' : 'text-ink',
                ].join(' ')}
              >
                {r.name}
              </p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}

function SiteFooter() {
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-5 px-6 py-10 sm:flex-row">
        <Wordmark />
        <p className="max-w-md text-center text-xs leading-relaxed text-ink-faint sm:text-right">
          {t.landing.disclaimer}
        </p>
      </div>
    </footer>
  )
}
