'use client'

import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { track } from '@/lib/analytics'
import { astrologyApi, type PdfStatus } from '@/lib/astrology-api'
import { useLocale } from '@/lib/i18n/context'
import { cn } from '@/lib/utils'

/**
 * The download button, and the polling behind it.
 *
 * ── Why this is not a link ──
 *
 * There is no URL to link to until a worker has rendered one. The button
 * asks for a render, gets a job id back, and polls until the job carries
 * a signed URL. That is three states the user can be in, and each of
 * them has to say something: a button that silently does nothing for
 * eight seconds reads as broken.
 *
 * ── Polling, not a socket ──
 *
 * A WebSocket exists in this product from Phase 7 and would be the wrong
 * tool here anyway: this is one short-lived question with a known
 * answer, asked by one client, a handful of times. Polling costs a few
 * requests and no connection state.
 */

/**
 * How long between polls.
 *
 * A render is seconds, so a second is responsive without being wasteful.
 * The cap below is what stops a stuck job polling forever — the server
 * bounds the render at ninety seconds and marks it failed, so a client
 * that has waited appreciably longer than that is polling a job whose
 * status write itself failed.
 */
const POLL_INTERVAL_MS = 1_000
const POLL_LIMIT = 120

type Phase = 'idle' | 'working' | 'ready' | 'failed'

export function DownloadPdf({
  profileId,
  className,
}: {
  profileId: string
  className?: string
}) {
  const { t, locale } = useLocale()

  const [phase, setPhase] = useState<Phase>('idle')
  const [url, setUrl] = useState<string | null>(null)

  /*
    The poll is cancelled when this unmounts.

    Without it, navigating away mid-render leaves a timer calling setState
    on a gone component — which React warns about, and which would also
    keep requesting a status nobody is waiting for.
  */
  const cancelled = useRef(false)
  useEffect(() => {
    cancelled.current = false
    return () => {
      cancelled.current = true
    }
  }, [])

  const poll = useCallback(
    async (jobId: string) => {
      for (let attempt = 0; attempt < POLL_LIMIT; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS))
        if (cancelled.current) return

        let status: PdfStatus
        try {
          status = await astrologyApi.pdfStatus(profileId, jobId)
        } catch {
          // One failed poll is not a failed render — a dropped request on
          // a phone changing cell is ordinary. Keep asking; the limit
          // above is what eventually gives up.
          continue
        }
        if (cancelled.current) return

        if (status.status === 'done' && status.url) {
          setUrl(status.url)
          setPhase('ready')
          track('kundli_pdf_ready', {})
          return
        }
        if (status.status === 'failed') {
          setPhase('failed')
          track('kundli_pdf_failed', {})
          return
        }
      }

      // Out of attempts. Reported as a failure rather than left spinning:
      // the render is bounded server-side, so past this point nothing is
      // going to arrive.
      if (!cancelled.current) {
        setPhase('failed')
        track('kundli_pdf_failed', {})
      }
    },
    [profileId],
  )

  const start = useCallback(async () => {
    setPhase('working')
    setUrl(null)
    track('kundli_pdf_requested', {})

    try {
      const { job_id } = await astrologyApi.requestPdf(profileId, locale)
      await poll(job_id)
    } catch {
      if (!cancelled.current) {
        setPhase('failed')
        track('kundli_pdf_failed', {})
      }
    }
  }, [locale, poll, profileId])

  if (phase === 'ready' && url) {
    return (
      <div className={cn('flex flex-col items-start gap-1', className)}>
        {/*
          A real anchor, not a button that navigates. The link is a URL
          the user may well want to copy, open in another tab or send to
          themselves, and an onClick handler supports none of that.

          `rel="noopener"` because `target="_blank"` otherwise hands the
          opened page a reference to this window.
        */}
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className={cn(
            'inline-flex items-center gap-2 rounded-md border border-gold/40 px-4 py-2',
            'text-sm font-medium text-gold hover:bg-gold/10',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            'focus-visible:ring-offset-2 focus-visible:ring-offset-background',
          )}
        >
          {/* A glyph as well as the colour — colour alone carries no
              meaning for roughly 8% of men, who read status too. */}
          <span aria-hidden="true">↓</span>
          {t.chart.downloadOpen}
        </a>
        <p className="text-xs text-ink-muted">{t.chart.downloadExpires}</p>
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col items-start gap-1', className)}>
      <Button
        type="button"
        variant="outline"
        onClick={() => void start()}
        disabled={phase === 'working'}
        // The live region is on the message below rather than here, so a
        // screen reader is told the state changed rather than having the
        // button's own label read out again.
        aria-busy={phase === 'working'}
      >
        {phase === 'working' ? t.chart.downloadPreparing : t.chart.downloadPdf}
      </Button>

      {/*
        One live region, always present.

        Rendering it only while there is something to say means the region
        itself is new when the text appears, and a region that did not
        exist a moment ago is not reliably announced.
      */}
      <p
        role="status"
        aria-live="polite"
        className={cn('text-xs', phase === 'failed' ? 'text-danger' : 'text-ink-muted')}
      >
        {phase === 'failed' ? t.chart.downloadFailed : ''}
      </p>
    </div>
  )
}
