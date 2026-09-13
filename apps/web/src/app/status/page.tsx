import Link from 'next/link'
import { Wordmark } from '@/components/Logo'
import { Badge, Card, SectionLabel, StatusDot, cx } from '@/components/ui'
import {
  api,
  ApiUnreachableError,
  type HealthResponse,
  type MetaResponse,
} from '@/lib/api'

// Always render fresh. A cached status page is a lying status page.
export const dynamic = 'force-dynamic'

type Fetched<T> = { ok: true; data: T } | { ok: false; error: string }

async function attempt<T>(fn: () => Promise<T>): Promise<Fetched<T>> {
  try {
    return { ok: true, data: await fn() }
  } catch (err) {
    const message =
      err instanceof ApiUnreachableError
        ? 'api-service is unreachable'
        : err instanceof Error
          ? err.message
          : 'Unknown error'
    return { ok: false, error: message }
  }
}

export default async function StatusPage() {
  // Concurrent, and each failing independently: if /meta is broken we
  // still want to see /health.
  const [health, meta] = await Promise.all([
    attempt(api.health),
    attempt(api.meta),
  ])

  return (
    <>
      <header className="border-b border-border/60">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-5">
          <Link href="/" aria-label="Back to home">
            <Wordmark />
          </Link>
          <Link
            href="/"
            className="rounded-lg px-3 py-2 text-sm text-ink-muted transition-colors hover:text-ink"
          >
            ← Home
          </Link>
        </div>
      </header>

      <main id="main" className="mx-auto max-w-4xl px-6 py-14">
        <div className="mb-10">
          <SectionLabel>Operations</SectionLabel>
          <h1 className="font-serif text-4xl tracking-tight">System status</h1>
          <p className="mt-3 max-w-2xl text-sm leading-relaxed text-ink-muted">
            Every dependency reported independently. With three backend
            services plus Postgres, Redis and object storage,{' '}
            <em className="text-ink">&ldquo;the API is down&rdquo;</em> is not
            an actionable statement — so this page never says that.
          </p>
        </div>

        <OverallBanner health={health} />
        <Dependencies health={health} />
        <BuildInfo meta={meta} />
      </main>
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────

function OverallBanner({ health }: { health: Fetched<HealthResponse> }) {
  if (!health.ok) {
    return (
      <Card className="mb-8 border-danger/35 bg-danger/[0.06]">
        <div className="flex items-start gap-3">
          <StatusDot status="error" />
          <div>
            <h2 className="font-serif text-xl text-danger">
              Cannot reach api-service
            </h2>
            <p className="mt-1.5 text-sm text-ink-muted">{health.error}</p>
            <p className="mt-3 text-xs text-ink-faint">
              Expected at{' '}
              <code className="rounded bg-base px-1.5 py-0.5 font-mono">
                {api.url}
              </code>
              . Start it with{' '}
              <code className="rounded bg-base px-1.5 py-0.5 font-mono">
                task dev:api
              </code>
              .
            </p>
          </div>
        </div>
      </Card>
    )
  }

  const { status } = health.data
  const tone =
    status === 'ok'
      ? { border: 'border-ok/35 bg-ok/[0.06]', text: 'text-ok', label: 'All systems operational' }
      : status === 'degraded'
        ? { border: 'border-warn/35 bg-warn/[0.06]', text: 'text-warn', label: 'Degraded — some services unavailable' }
        : { border: 'border-danger/35 bg-danger/[0.06]', text: 'text-danger', label: 'Outage — a critical dependency is down' }

  return (
    <Card className={cx('mb-8', tone.border)}>
      <div className="flex items-center gap-3">
        <StatusDot status={status} />
        <h2 className={cx('font-serif text-xl', tone.text)}>{tone.label}</h2>
      </div>
    </Card>
  )
}

/**
 * Human-readable descriptions, so the page explains the architecture as
 * well as reporting on it.
 */
const DEPENDENCY_INFO: Record<
  string,
  { label: string; detail: string; critical: boolean }
> = {
  postgres: {
    label: 'PostgreSQL',
    detail: 'Primary datastore with pgvector. api-service is the only writer.',
    critical: true,
  },
  redis: {
    label: 'Redis',
    detail: 'Cache, rate-limit windows and short-lived secrets.',
    critical: true,
  },
  astro: {
    label: 'astro-service',
    detail: 'Deterministic chart computation. Python, stateless, no AI access.',
    critical: false,
  },
  ai: {
    label: 'ai-service',
    detail: 'LLM orchestration and retrieval. Python, read-only database role.',
    critical: false,
  },
  storage: {
    label: 'Object storage',
    detail: 'S3-compatible. PDF reports and generated assets from Phase 3.',
    critical: false,
  },
  mail: {
    label: 'Mail',
    detail: 'Transactional email. Carries login OTPs from Phase 1.',
    critical: false,
  },
}

function Dependencies({ health }: { health: Fetched<HealthResponse> }) {
  if (!health.ok) return null

  const entries = Object.entries(health.data.checks)

  if (entries.length === 0) {
    return (
      <Card className="mb-8">
        <p className="text-sm text-ink-muted">No dependency checks reported.</p>
      </Card>
    )
  }

  return (
    <section className="mb-8">
      <SectionLabel>Dependencies</SectionLabel>
      <div className="space-y-2.5">
        {entries.map(([name, check]) => {
          const info = DEPENDENCY_INFO[name] ?? {
            label: name,
            detail: '',
            critical: false,
          }

          return (
            <Card key={name} className="p-4">
              <div className="flex items-start justify-between gap-4">
                <div className="flex min-w-0 items-start gap-3">
                  <span className="mt-1.5">
                    <StatusDot status={check.status} />
                  </span>
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="font-medium">{info.label}</h3>
                      {!info.critical && (
                        <Badge tone="neutral" className="text-[10px]">
                          non-critical
                        </Badge>
                      )}
                    </div>
                    {info.detail && (
                      <p className="mt-1 text-xs leading-relaxed text-ink-muted">
                        {info.detail}
                      </p>
                    )}
                    {check.error && (
                      <p className="mt-2 break-words font-mono text-xs text-danger">
                        {check.error}
                      </p>
                    )}
                  </div>
                </div>

                <div className="shrink-0 text-right">
                  <p
                    className={cx(
                      'text-sm font-medium',
                      check.status === 'ok' ? 'text-ok' : 'text-danger',
                    )}
                  >
                    {check.status}
                  </p>
                  <p className="font-mono text-xs text-ink-faint">
                    {check.latency_ms}ms
                  </p>
                </div>
              </div>
            </Card>
          )
        })}
      </div>

      <p className="mt-4 text-xs leading-relaxed text-ink-faint">
        A non-critical dependency failing degrades the system rather than
        downing it. Cached charts still render when astro-service is
        unavailable, and everything except chat still works without
        ai-service — that is a deliberate design property, not an accident.
      </p>
    </section>
  )
}

function BuildInfo({ meta }: { meta: Fetched<MetaResponse> }) {
  if (!meta.ok) return null

  const { data } = meta
  const features = Object.entries(data.features)

  return (
    <section>
      <SectionLabel>Build</SectionLabel>
      <Card>
        <dl className="grid gap-4 sm:grid-cols-3">
          {[
            ['Service', data.service],
            ['Version', data.version],
            ['Environment', data.env],
          ].map(([k, v]) => (
            <div key={k}>
              <dt className="text-xs text-ink-faint">{k}</dt>
              <dd className="mt-0.5 font-mono text-sm">{v}</dd>
            </div>
          ))}
          <div className="sm:col-span-3">
            <dt className="text-xs text-ink-faint">Phase</dt>
            <dd className="mt-0.5 text-sm text-gold">{data.phase}</dd>
          </div>
        </dl>

        <div className="mt-6 border-t border-border pt-5">
          <p className="mb-3 text-xs text-ink-faint">Feature flags</p>
          <div className="flex flex-wrap gap-2">
            {features.map(([name, enabled]) => (
              <Badge key={name} tone={enabled ? 'ok' : 'neutral'}>
                <span aria-hidden="true">{enabled ? '●' : '○'}</span>
                <span className="font-mono text-[11px]">{name}</span>
              </Badge>
            ))}
          </div>
          <p className="mt-3 text-xs text-ink-faint">
            Everything is off in Phase 0. Each flag switches on as its phase
            gate passes.
          </p>
        </div>
      </Card>
    </section>
  )
}
