'use client'

import { use, useEffect, useState } from 'react'

import { ChartSVG } from '@/components/chart/ChartSVG'
import { parseChartData } from '@/components/chart/parse'
import type { ChartData } from '@/components/chart/types'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { useLocale } from '@/lib/i18n/context'

/**
 * A chart somebody sent you.
 *
 * ── The only page in the product with no account behind it ──
 *
 * Everything else here assumes a signed-in user. This does not: the
 * person opening the link is somebody's aunt, or an astrologer, and they
 * are not going to make an account to look at a chart. The opaque token
 * in the URL is the whole credential.
 *
 * ── What it deliberately does not show ──
 *
 * No birth date, no time, no place. Those are not omitted by this
 * component's choice — the API does not send them, and this page could
 * not display them if it wanted to. That is the right place for the
 * decision: a payload that cannot carry the data cannot be made to leak
 * it by a future change here.
 *
 * ── No sign-in prompt ──
 *
 * There is a quiet temptation to gate this behind "sign up to see the
 * chart", because it is the product's best organic-acquisition surface.
 * It is not done, and should not be: the owner shared a chart, not a
 * lead. A link that demands an account before honouring what its sender
 * promised is a worse product and a broken promise.
 */

interface SharedResponse {
  scope: string
  chart: {
    label: string
    chart_type: string
    chart_data: unknown
    ayanamsa: string
    house_system: string
    engine_version: string
  }
}

type State = 'loading' | 'ready' | 'gone' | 'unreadable'

export default function SharedChartPage({
  params,
}: {
  params: Promise<{ token: string }>
}) {
  const { token } = use(params)
  const { t } = useLocale()

  const [fetchState, setFetchState] = useState<State>('loading')
  const [chart, setChart] = useState<ChartData | null>(null)
  const [label, setLabel] = useState('')

  /*
    A missing token is derived, not stored.

    Setting state for it inside an effect would be a second render for
    something already known during the first — and the lint rule that
    forbids it is right: this is a property of the URL, not state.
  */
  const state: State = token ? fetchState : 'gone'

  useEffect(() => {
    if (!token) return

    const controller = new AbortController()

    fetch(`${api.url}/api/v1/shared/${encodeURIComponent(token)}`, {
      signal: controller.signal,
      // No credentials. A developer opening this in a browser with a
      // live session must not get a result the recipient would not —
      // which would make the route look correct while being untested for
      // the only caller that matters.
      credentials: 'omit',
      headers: { Accept: 'application/json' },
    })
      .then(async (res) => {
        if (res.status === 404) {
          // Expired, revoked and never-existed are one answer from the
          // API, and one message here. Telling a viewer WHICH would tell
          // them a link was once real and its owner turned it off.
          setFetchState('gone')
          return null
        }
        if (!res.ok) {
          setFetchState('unreadable')
          return null
        }
        return (await res.json()) as SharedResponse
      })
      .then((body) => {
        if (!body) return
        const parsed = parseChartData(body.chart.chart_data)
        if (!parsed) {
          setFetchState('unreadable')
          return
        }
        setChart(parsed)
        setLabel(body.chart.label)
        setFetchState('ready')
      })
      .catch(() => {
        if (!controller.signal.aborted) setFetchState('unreadable')
      })

    return () => controller.abort()
  }, [token])

  return (
    <main id="main" tabIndex={-1} className="mx-auto max-w-2xl px-4 py-8 sm:px-6">
      <header className="mb-6">
        <h1 className="font-serif text-2xl">{t.chart.sharedTitle}</h1>
        {state === 'ready' && label && (
          <p className="mt-1 text-sm text-ink-muted">{label}</p>
        )}
      </header>

      {state === 'loading' && (
        // A skeleton shaped like a chart rather than a spinner, so the
        // page does not visibly change size when the content lands.
        <div aria-busy="true" aria-live="polite">
          <Skeleton className="aspect-square w-full rounded-md" />
        </div>
      )}

      {(state === 'gone' || state === 'unreadable') && (
        <p role="status" className="text-sm text-ink-muted">
          {t.chart.sharedGone}
        </p>
      )}

      {state === 'ready' && chart && (
        <>
          <ChartSVG
            chart={chart}
            style="north"
            captionText={t.chart.srTableCaption}
            className="w-full"
          />

          {/*
            An honest footer, not a call to action.

            Naming the product is fair — somebody seeing a chart they like
            should be able to find out what made it. Interrupting them
            with a sign-up wall is not, and it is the obvious thing this
            page will be asked to grow later.
          */}
          <p className="mt-8 text-center text-xs text-ink-faint">
            {t.chart.sharedMadeWith}
          </p>
        </>
      )}
    </main>
  )
}
