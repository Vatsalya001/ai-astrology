'use client'

import { useSearchParams } from 'next/navigation'
import { Suspense, useEffect, useMemo, useState } from 'react'

import { ChartSVG } from '@/components/chart/ChartSVG'
import { formatDegree } from '@/components/chart/format'
import { parseChartData } from '@/components/chart/parse'
import type { ChartData } from '@/components/chart/types'
import { useLocale } from '@/lib/i18n/context'
import {
  fetchPrintBundle,
  PrintTokenRejectedError,
  type PrintBundle,
  type PrintPeriod,
} from '@/lib/print-api'

/**
 * The page headless Chrome prints.
 *
 * ── Why this exists rather than a PDF library ──
 *
 * The document is the product's own screen, printed. A Go PDF library
 * would mean a second implementation of the chart — its geometry, its
 * glyph placement, its typography — maintained alongside the one people
 * actually look at, and the two would drift silently, because nobody
 * checks a PDF as often as a screen.
 *
 * So this route renders the same `ChartSVG` the app renders, and the
 * worker prints it.
 *
 * ── It has no session ──
 *
 * The browser that loads this is a subprocess on the worker: no cookies,
 * no localStorage, no signed-in user. Its credential is the single-use
 * token in the query string, which it hands straight to the one API
 * endpoint that accepts one. Everything else on this page is derived
 * from that single response, because redeeming the token spends it.
 *
 * ── `data-print-ready` is the contract with the worker ──
 *
 * chromedp waits for `body[data-print-ready="true"]` before calling
 * PrintToPDF. A fixed sleep would be the obvious alternative and is
 * wrong in both directions: too short on a cold worker and the PDF is
 * missing its chart, too long and every render pays the worst case.
 *
 * The flag is set only after the fonts have settled, because the chart's
 * glyphs are text: printing before they load produces a page of
 * fallback boxes, which is a document that looks broken rather than one
 * that is obviously incomplete.
 *
 * ── Why the Suspense boundary ──
 *
 * `useSearchParams` needs one above it, so the page is the boundary and
 * the work is one level down. Without it `next build` refuses the route:
 * reading the query string forces client rendering, and Next wants the
 * fallback declared rather than inferred. That fallback is never what
 * the worker prints — it does not shoot until `data-print-ready`, which
 * only the real document sets.
 */
export default function PrintPage() {
  return (
    <Suspense fallback={<PrintShell>{null}</PrintShell>}>
      <PrintDocument />
    </Suspense>
  )
}

function PrintDocument() {
  const { t, fill, locale, setLocale } = useLocale()
  const searchParams = useSearchParams()

  const token = searchParams.get('token')
  const wantedLocale = searchParams.get('locale')

  const [bundle, setBundle] = useState<PrintBundle | null>(null)
  const [fetchFailure, setFetchFailure] = useState<'invalid' | 'error' | null>(null)
  const [ready, setReady] = useState(false)

  /*
    A missing token is derived, not stored.

    Setting state for it inside an effect would be a second render for a
    fact already known during the first, and the lint rule that forbids
    it is right: this is not state, it is a property of the URL.
  */
  const failure: 'invalid' | 'error' | null = token ? fetchFailure : 'invalid'

  /*
    The locale comes from the URL, not from storage.

    Every other screen reads `ayana.locale` out of localStorage. This one
    has none — the worker starts a fresh browser profile per render — so
    the language would silently fall back to English for every Hindi
    user's PDF, with nothing failing anywhere.

    setLocale writes storage and notifies an external store, which is
    what an effect is for: this is synchronising React with something
    outside it, not deriving state.
  */
  useEffect(() => {
    if ((wantedLocale === 'hi' || wantedLocale === 'en') && wantedLocale !== locale) {
      setLocale(wantedLocale)
    }
  }, [wantedLocale, locale, setLocale])

  useEffect(() => {
    if (!token) return

    const controller = new AbortController()
    fetchPrintBundle(token, controller.signal)
      .then(setBundle)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setFetchFailure(err instanceof PrintTokenRejectedError ? 'invalid' : 'error')
      })
    return () => controller.abort()
  }, [token])

  /*
    Ready means laid out AND lettered.

    `document.fonts.ready` is the part that matters: the chart's planet
    glyphs and every degree are text, and Chrome will happily print a
    page whose webfonts are still in flight. The result is a document
    full of fallback boxes — which is worse than a failed render,
    because it succeeds and the user keeps the file.

    The double rAF after it waits for the browser to have actually
    painted with those fonts rather than merely having them available.
  */
  useEffect(() => {
    if (!bundle && !failure) return

    let cancelled = false
    const settle = () => {
      if (cancelled) return
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          if (!cancelled) setReady(true)
        })
      })
    }

    if (typeof document !== 'undefined' && 'fonts' in document) {
      void document.fonts.ready.then(settle)
    } else {
      settle()
    }

    return () => {
      cancelled = true
    }
  }, [bundle, failure])

  /*
    The flag goes on <body>, because that is what the worker waits for.

    On the element rather than in React's tree: chromedp's selector runs
    against the real document, and Next.js owns <body> from the root
    layout. Writing it here keeps the contract in the file that fulfils
    it instead of in a layout shared with every other page.
  */
  useEffect(() => {
    if (!ready) return
    document.body.setAttribute('data-print-ready', 'true')
    return () => {
      document.body.removeAttribute('data-print-ready')
    }
  }, [ready])

  if (failure) {
    /*
      A failure still sets `data-print-ready`, deliberately.

      Otherwise the worker waits the full ninety seconds for a page that
      has already given up, and the user waits with it. A one-page PDF
      saying the link expired is a worse document and a much better
      experience than a spinner that resolves into a timeout.
    */
    return (
      <PrintShell>
        <p className="text-base text-ink-print">
          {failure === 'invalid' ? t.chart.printInvalid : t.chart.printFailed}
        </p>
      </PrintShell>
    )
  }

  if (!bundle) {
    // Never printed: the worker does not shoot until `data-print-ready`,
    // and that is not set while this is on screen. It exists so a
    // developer opening the route by hand sees something.
    return (
      <PrintShell>
        <p className="text-base text-ink-print/70">{t.chart.loading}</p>
      </PrintShell>
    )
  }

  return <Document bundle={bundle} t={t} fill={fill} locale={locale} />
}

/**
 * The page frame.
 *
 * White background and dark ink, which is the one place in this app that
 * inverts the palette. The rest of the product is a midnight-navy
 * screen; this is a sheet of A4 someone may actually put through a
 * printer, and a full-bleed navy page costs a cartridge to produce
 * something harder to read than the default.
 *
 * The CHARTS keep their own dark panel, and that is deliberate rather
 * than an inconsistency. `ChartSVG` draws a `fill-surface` field with
 * `fill-ink` glyphs on it: the contrast is designed as a unit, and the
 * whole thing is a bounded figure on the page rather than the page
 * itself. Re-tinting it for print would mean a second colour scheme for
 * the chart, maintained against thirty fixture tests that assert on the
 * first — for a diagram that already reads correctly on paper.
 */
function PrintShell({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto min-h-screen max-w-[780px] bg-white px-10 py-8 text-ink-print">
      {children}
    </main>
  )
}

function Document({
  bundle,
  t,
  fill,
  locale,
}: {
  bundle: PrintBundle
  t: ReturnType<typeof useLocale>['t']
  fill: ReturnType<typeof useLocale>['fill']
  locale: string
}) {
  const profile = bundle.birth_profile

  const charts = useMemo(() => {
    const parse = (type: string): ChartData | null => {
      const raw = bundle.charts[type]
      return raw ? parseChartData(raw.chart_data) : null
    }
    return { D1: parse('D1'), D9: parse('D9'), D10: parse('D10') }
  }, [bundle])

  const born = profile.birth_time
    ? fill(t.chart.printBornOn, {
        date: formatDate(profile.birth_date, locale),
        time: profile.birth_time,
        place: profile.birth_place,
      })
    : fill(t.chart.printBornOnNoTime, {
        date: formatDate(profile.birth_date, locale),
        place: profile.birth_place,
      })

  const rasiMeta = bundle.charts.D1

  return (
    <PrintShell>
      <header className="mb-8 border-b border-ink-print/20 pb-4">
        <h1 className="text-3xl font-semibold tracking-tight">{t.chart.printTitle}</h1>
        <p className="mt-1 text-lg">{fill(t.chart.printFor, { name: profile.label })}</p>
        <p className="mt-1 text-sm text-ink-print/70">{born}</p>
      </header>

      {charts.D1 && (
        <ChartSection
          title={t.chart.printRasi}
          chart={charts.D1}
          caption={t.chart.srTableCaption}
        />
      )}

      {charts.D1 && (
        <section className="mb-8 break-inside-avoid">
          <h2 className="mb-3 text-xl font-medium">{t.chart.printPositions}</h2>
          <PositionsTable chart={charts.D1} t={t} />
        </section>
      )}

      {charts.D9 && (
        <ChartSection
          title={t.chart.printNavamsa}
          chart={charts.D9}
          caption={t.chart.srTableCaption}
        />
      )}

      {charts.D10 && (
        <ChartSection
          title={t.chart.printDasamsa}
          chart={charts.D10}
          caption={t.chart.srTableCaption}
        />
      )}

      <section className="mb-8 break-inside-avoid">
        <h2 className="mb-3 text-xl font-medium">{t.chart.printDashaTitle}</h2>
        {bundle.mahadashas && bundle.mahadashas.length > 0 ? (
          <DashaTable
            periods={bundle.mahadashas}
            currentID={bundle.current_dasha?.mahadasha?.id ?? null}
            currentLabel={t.chart.printDashaCurrent}
            locale={locale}
          />
        ) : (
          <p className="text-sm text-ink-print/70">{t.chart.printNoDashas}</p>
        )}
      </section>

      <footer className="mt-10 border-t border-ink-print/20 pt-4 text-xs text-ink-print/60">
        <p>{fill(t.chart.printGeneratedAt, { date: formatDate(bundle.generated_at, locale) })}</p>
        {rasiMeta && (
          <p className="mt-1">
            {fill(t.chart.printEngine, {
              engine: rasiMeta.engine_version,
              ayanamsa: rasiMeta.ayanamsa,
            })}
          </p>
        )}
        <p className="mt-3 leading-relaxed">{t.chart.printDisclaimer}</p>
      </footer>
    </PrintShell>
  )
}

/**
 * One chart, with its heading kept on the same page as its diagram.
 *
 * `break-inside-avoid` matters more here than anywhere else on the page:
 * a chart split across a page boundary is not a chart, it is two halves
 * of a square.
 */
function ChartSection({
  title,
  chart,
  caption,
}: {
  title: string
  chart: ChartData
  caption: string
}) {
  return (
    <section className="mb-8 break-inside-avoid">
      <h2 className="mb-3 text-xl font-medium">{title}</h2>
      <ChartSVG
        chart={chart}
        style="north"
        className="mx-auto w-full max-w-[460px]"
        title={title}
        captionText={caption}
      />
    </section>
  )
}

/**
 * The positions table.
 *
 * Written out here rather than reusing `PlanetTable`, and that is a real
 * trade rather than an oversight. `PlanetTable` is an interactive
 * component: every row is a button that opens a sheet. On paper a button
 * is a row that looks tappable and does nothing, and the sheet's content
 * never appears at all. So the printed version is a plain table.
 *
 * The DATA is the same object either way, so the two cannot disagree
 * about a position — only about how it is presented.
 */
function PositionsTable({
  chart,
  t,
}: {
  chart: ChartData
  t: ReturnType<typeof useLocale>['t']
}) {
  return (
    <table className="w-full border-collapse text-sm">
      <thead>
        <tr className="border-b border-ink-print/30 text-left">
          <th className="py-1.5 pr-3 font-medium">{t.chart.colPlanet}</th>
          <th className="py-1.5 pr-3 font-medium">{t.chart.colSign}</th>
          <th className="py-1.5 pr-3 font-medium">{t.chart.colDegree}</th>
          <th className="py-1.5 pr-3 font-medium">{t.chart.colHouse}</th>
          <th className="py-1.5 font-medium">{t.chart.colNakshatra}</th>
        </tr>
      </thead>
      <tbody>
        {chart.planets.map((planet) => (
          <tr key={planet.planet} className="border-b border-ink-print/10">
            <td className="py-1.5 pr-3">{planet.planet}</td>
            <td className="py-1.5 pr-3">{planet.sign}</td>
            <td className="py-1.5 pr-3 tabular-nums">{formatDegree(planet.degree)}</td>
            <td className="py-1.5 pr-3 tabular-nums">{planet.house ?? '—'}</td>
            <td className="py-1.5">{planet.nakshatra}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/**
 * The mahadasha sequence as a table rather than the interactive timeline.
 *
 * `DashaTimeline` is proportional bars you drill into. Printed, the
 * proportions survive and the drilling does not — so the reader gets a
 * row of unlabelled rectangles they cannot open. Dates in a table carry
 * the same information and read better on paper.
 *
 * The current period is marked with a word as well as a weight. Colour
 * and boldness alone carry no meaning for a colour-blind reader, and on
 * a monochrome printer they carry none for anybody.
 */
function DashaTable({
  periods,
  currentID,
  currentLabel,
  locale,
}: {
  periods: PrintPeriod[]
  currentID: string | null
  currentLabel: string
  locale: string
}) {
  return (
    <table className="w-full border-collapse text-sm">
      <tbody>
        {periods.map((period) => {
          const isCurrent = period.id === currentID
          return (
            <tr
              key={period.id}
              className={`border-b border-ink-print/10 ${isCurrent ? 'font-semibold' : ''}`}
            >
              <td className="py-1.5 pr-3">{period.planet}</td>
              <td className="py-1.5 pr-3 tabular-nums">
                {formatDate(period.start, locale)} – {formatDate(period.end, locale)}
              </td>
              <td className="py-1.5 text-right">{isCurrent ? currentLabel : ''}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

/**
 * Dates, in the document's own locale.
 *
 * `Intl` rather than a hand-rolled format: a Hindi PDF with English
 * month names is the kind of half-translation that reads as carelessness
 * rather than as a missing feature.
 *
 * UTC, not the viewer's zone. The worker's container runs in whatever
 * timezone its image was built with, and a birth date that shifts by a
 * day depending on which machine rendered the PDF is a bug nobody would
 * ever reproduce.
 */
function formatDate(iso: string, locale: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat(locale === 'hi' ? 'hi-IN' : 'en-IN', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(date)
}
